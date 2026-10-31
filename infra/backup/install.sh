#!/usr/bin/env bash
# Installs the nightly backup on a server prepared by this repo: mounts the
# backup disk BACKUP_DEVICE at BACKUP_DIR, puts backup.sh with its settings
# from the server's settings file in /srv/backup, and a systemd timer that
# runs it at about 03:30 UTC. --now also runs one backup at once and shows how
# it went.
#
#   ./backup/install.sh deploy@SERVER [--now]
set -euo pipefail
host=${1:?usage: ./backup/install.sh deploy@SERVER [--now]}
user=${host%%@*}
cd "$(dirname "$0")"
settings=$(../env.sh backup "$host")
dir=$(sed -n 's/^BACKUP_DIR=//p' <<<"$settings")
device=$(sed -n 's/^BACKUP_DEVICE=//p' <<<"$settings")
if [ -z "$dir" ] || [ -z "$device" ]; then
  echo "BACKUP_DIR and BACKUP_DEVICE go under [backup] in the server's settings file" >&2
  exit 1
fi
# The disk is mounted by its filesystem's UUID, which stays the same when the
# volume moves to a rebuilt server.
ssh "$host" bash -s -- "$dir" "$device" "$user" <<'MOUNT'
set -euo pipefail
dir=$1 device=$2 user=$3
uuid=$(sudo blkid -s UUID -o value "$device")
[ -n "$uuid" ]
sudo install -d "$dir"
grep -q "^UUID=$uuid " /etc/fstab || echo "UUID=$uuid $dir ext4 defaults,nofail,discard 0 2" | sudo tee -a /etc/fstab >/dev/null
sudo systemctl daemon-reload
mountpoint -q "$dir" || sudo mount "$dir"
sudo chown "$user:$user" "$dir"
sudo chmod 700 "$dir"
sudo install -d -m 700 -o "$user" -g "$user" /srv/backup
MOUNT
scp -q backup.sh "$host:/srv/backup/backup.sh"
ssh "$host" "chmod 700 /srv/backup/backup.sh && umask 077 && cat >/srv/backup/.env" <<<"$settings"
ssh "$host" "sudo tee /etc/systemd/system/candylario-backup.service >/dev/null" <<UNIT
[Unit]
Description=Candylario backup
After=docker.service
RequiresMountsFor=$dir

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
