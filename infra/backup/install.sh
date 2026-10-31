#!/usr/bin/env bash
# Installs the nightly backup on a server prepared by this repo: age and
# rclone, backup.sh with its settings from the server's settings file (not
# the secret half of the backup key), and a systemd timer that runs it at about
# 03:30 UTC. --now also runs one backup at once and shows how it went.
#
#   ./backup/install.sh deploy@SERVER [--now]
set -euo pipefail
host=${1:?usage: ./backup/install.sh deploy@SERVER [--now]}
user=${host%%@*}
cd "$(dirname "$0")"
settings=$(../env.sh backup "$host")
ssh "$host" "sudo apt-get -o DPkg::Lock::Timeout=600 -qq update && sudo apt-get -o DPkg::Lock::Timeout=600 -yqq install age rclone >/dev/null && sudo install -d -m 700 -o $user -g $user /srv/backup"
scp -q backup.sh "$host:/srv/backup/backup.sh"
ssh "$host" "chmod 700 /srv/backup/backup.sh && umask 077 && cat >/srv/backup/.env" <<<"$settings"
ssh "$host" "sudo tee /etc/systemd/system/candylario-backup.service >/dev/null" <<UNIT
[Unit]
Description=Candylario backup
After=docker.service

[Service]
Type=oneshot
User=$user
ExecStart=/srv/backup/backup.sh
UNIT
ssh "$host" "sudo tee /etc/systemd/system/candylario-backup.timer >/dev/null" <<UNIT
[Unit]
Description=Candylario backup every night

[Timer]
OnCalendar=*-*-* 03:30:00 UTC
RandomizedDelaySec=20m
Persistent=true

[Install]
WantedBy=timers.target
UNIT
ssh "$host" "sudo systemctl daemon-reload && sudo systemctl enable --now candylario-backup.timer 2>/dev/null"
if [ "${2:-}" = --now ]; then
  ssh "$host" "sudo systemctl start candylario-backup.service; journalctl -u candylario-backup -n 3 --no-pager -o cat"
fi
echo "Backups run nightly on $host. Check one with ./backup/restore.sh $host"
