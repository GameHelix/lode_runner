#!/usr/bin/env bash
# Rebuilds the server on the same IPv4 address, after a failure or as a
# rehearsal. It snapshots the server at Hetzner, stops the services and backs
# them up to the backup volume, has OpenTofu replace the server, then on the
# new one puts /srv/secrets back from the volume, deploys the three services
# and restores their databases from that backup. --plan only shows what
# OpenTofu would do. The rebuild workflow runs it; it prints how long each step
# took. If it fails after the old server is gone, the OpenTofu state stays in
# this directory: the workflow keeps it as an artifact.
#
#   ./hetzner/rebuild.sh deploy@SERVER [--plan]
# shellcheck disable=SC2029 # the paths are meant to expand here, before ssh.
set -euo pipefail
host=${1:?usage: ./hetzner/rebuild.sh deploy@SERVER [--plan]}
user=${host%%@*} ip=${host#*@}
cd "$(dirname "$0")"
remote=/srv/secrets/infra
began=$(date +%s)
step() { echo "== $(($(date +%s) - began))s: $*"; }

scp -q "$host:$remote/terraform.tfstate" "$host:$remote/terraform.tfvars" .
trap 'rm -f terraform.tfvars' EXIT
HCLOUD_TOKEN=$(ssh "$host" "sed -n 's/^HCLOUD_TOKEN=//p' /srv/secrets/candylario.env")
export HCLOUD_TOKEN
tofu init -input=false -lockfile=readonly >/dev/null
if [ "${2:-}" = --plan ]; then
  tofu plan -input=false -no-color -replace=hcloud_server.host
  rm -f terraform.tfstate
  exit
fi

api() { curl -fsS -H "Authorization: Bearer $HCLOUD_TOKEN" -H 'Content-Type: application/json' "https://api.hetzner.cloud/v1$1" "${@:2}"; }
settle() {
  local status
  for _ in $(seq 180); do
    status=$(api "/actions/$1" | jq -r .action.status)
    [ "$status" = success ] && return
    [ "$status" = error ] && return 1
    sleep 5
  done
  return 1
}

step "snapshot the server at Hetzner, in case the rebuild fails"
server=$(api /servers | jq -r --arg ip "$ip" '.servers[] | select(.public_net.ipv4.ip == $ip) | .id')
snapshot=$(api "/servers/$server/actions/create_image" -X POST -d '{"type":"snapshot","description":"before rebuild","labels":{"project":"candylario"}}')
settle "$(jq -r .action.id <<<"$snapshot")"
echo "snapshot $(jq -r .image.id <<<"$snapshot"); delete it once the new server works"

step "stop the services and back them up to the volume"
restart() { ssh "$host" 'for s in trader notifier app; do (cd "/srv/$s" && docker compose start "$s" </dev/null); done'; }
trap restart ERR
ssh "$host" 'for s in app notifier trader; do (cd "/srv/$s" && docker compose stop "$s" </dev/null); done; sudo systemctl start candylario-backup.service'
day=$(ssh "$host" /srv/backup/backup.sh days | tail -1)
[ "$day" = "$(date -u +%F)" ]
ssh "$host" 'sudo umount /mnt/backup'

step "replace the server; the address and the backup volume move to the new one"
trap - ERR
# Hetzner sometimes fails a volume attach; a second apply finishes the rest.
tofu apply -input=false -auto-approve -no-color -replace=hcloud_server.host ||
  tofu apply -input=false -auto-approve -no-color
device=$(tofu output -raw backup_device)

step "wait for the new server's first boot"
ssh-keygen -R "$ip" >/dev/null 2>&1 || true
for _ in $(seq 60); do
  ssh -o StrictHostKeyChecking=accept-new -o ConnectTimeout=5 -o BatchMode=yes "$host" true 2>/dev/null && break
  sleep 5
done
ssh "$host" 'cloud-init status --wait >/dev/null || true; cloud-init status' | grep -q 'status: done'

step "put /srv/secrets back from the volume"
ssh "$host" "sudo install -d /mnt/backup && sudo mount $device /mnt/backup && sudo tar -C /srv -xzf /mnt/backup/$day/secrets.tar.gz && sudo chown -R $user:$user /srv/secrets"
scp -q terraform.tfstate "$host:$remote/terraform.tfstate"
rm -f terraform.tfstate terraform.tfstate.backup

step "deploy the services"
for s in trader notifier app; do ../../deploy.sh "$s" "$host"; done

step "install the nightly backup and restore the databases from $day"
../backup/install.sh "$host"
for s in trader notifier app; do ../backup/restore.sh "$host" "$day" "$s" --live; done

step "check the site"
domain=$(../env.sh app "$host" | sed -n 's/^APP_DOMAIN=//p')
for _ in $(seq 30); do
  curl -fsS --max-time 10 "https://$domain/healthz" >/dev/null 2>&1 && break
  sleep 10
done
curl -fsS --max-time 10 "https://$domain/healthz"
echo
step "rebuilt. The new host key, for the DEPLOY_KNOWN_HOSTS secret:"
ssh-keyscan -t ed25519 "$ip" 2>/dev/null
