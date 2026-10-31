#!/usr/bin/env bash
# Rebuilds the server on the same IPv4 address, after a failure or as a
# rehearsal. It snapshots the server at Hetzner, stops the services and backs
# them up to the backup volume, has OpenTofu replace the server, then on the
# new one puts /srv/secrets back from the volume, deploys the three services
# and restores their databases from that backup. --plan only shows what
# OpenTofu would do. The rebuild workflow runs it; it prints how long each step
# took. If it fails after the old server is gone, the OpenTofu state stays in
# this directory: the workflow keeps it as an artifact, and --resume finishes
# the rebuild from it. --resume needs that state here and HCLOUD_TOKEN in the
# environment, since the server that held the token is gone; the tfvars are
# rebuilt from the state, which holds the SSH public keys, and the services
# come back from the newest backup on the volume. If Hetzner cannot place the
# server type asked for, a rebuild falls back to the type the server had.
#
#   ./hetzner/rebuild.sh deploy@SERVER [--plan|--resume]
# shellcheck disable=SC2029 # the paths are meant to expand here, before ssh.
set -euo pipefail
host=${1:?usage: ./hetzner/rebuild.sh deploy@SERVER [--plan|--resume]}
mode=${2:-}
user=${host%%@*} ip=${host#*@}
cd "$(dirname "$0")"
remote=/srv/secrets/infra
began=$(date +%s)
step() { echo "== $(($(date +%s) - began))s: $*"; }

trap 'rm -f terraform.tfvars' EXIT
if [ "$mode" = --resume ]; then
  [ -f terraform.tfstate ] || { echo "--resume needs the failed run's terraform.tfstate in $PWD" >&2; exit 1; }
  : "${HCLOUD_TOKEN:?--resume needs HCLOUD_TOKEN in the environment}"
  jq -r '[.resources[] | select(.type == "hcloud_ssh_key" and .name == "admin") | .instances[]
          | {i: .index_key, k: .attributes.public_key}] | sort_by(.i)
          | "ssh_public_keys = " + ([.[].k] | tojson)' terraform.tfstate >terraform.tfvars
else
  scp -q "$host:$remote/terraform.tfstate" "$host:$remote/terraform.tfvars" .
  HCLOUD_TOKEN=${HCLOUD_TOKEN:-$(ssh "$host" "sed -n 's/^HCLOUD_TOKEN=//p' /srv/secrets/candylario.env")}
fi
export HCLOUD_TOKEN
tofu init -input=false -lockfile=readonly >/dev/null
if [ "$mode" = --plan ]; then
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

day=
if [ "$mode" != --resume ]; then
step "snapshot the server at Hetzner, in case the rebuild fails"
server=$(api /servers | jq -r --arg ip "$ip" '.servers[] | select(.public_net.ipv4.ip == $ip) | .id')
snapshot=$(api "/servers/$server/actions/create_image" -X POST -d '{"type":"snapshot","description":"before rebuild","labels":{"project":"candylario"}}')
settle "$(jq -r .action.id <<<"$snapshot")"
echo "snapshot $(jq -r .image.id <<<"$snapshot"); delete it once the new server works"

step "stop the services and back them up to the volume"
restart() { ssh "$host" 'sudo mount /mnt/backup 2>/dev/null; for s in trader notifier app; do (cd "/srv/$s" && docker compose start "$s" </dev/null); done'; }
trap restart ERR
ssh "$host" 'for s in app notifier trader; do (cd "/srv/$s" && docker compose stop "$s" </dev/null); done; sudo systemctl start candylario-backup.service'
day=$(ssh "$host" /srv/backup/backup.sh days | tail -1)
[ "$day" = "$(date -u +%F)" ]
ssh "$host" 'sudo umount /mnt/backup'
fi

step "replace the server; the address and the backup volume move to the new one"
trap - ERR
# Empty, not a failure, when the state holds no server: the case --resume starts from.
state_of() { { tofu state show -no-color hcloud_server.host 2>/dev/null || true; } | awk -v k="$1" '$1 == k { gsub(/"/, "", $3); print $3 }'; }
before=$(state_of id)
old_type=$(state_of server_type)
if [ "$mode" = --resume ]; then
  tofu apply -input=false -auto-approve -no-color
elif ! tofu apply -input=false -auto-approve -no-color -replace=hcloud_server.host; then
  # While the old server still runs, nothing was replaced: start its
  # services again and stop. Once it is gone, a second plain apply finishes
  # what is left, such as a volume attach Hetzner failed. If that fails too,
  # Hetzner may have no server of the new type to place (resource_unavailable,
  # cx23 in fsn1 on 2026-10-31): build the type the old server was, which was
  # just freed in the same location.
  if [ "$(state_of id)" = "$before" ]; then
    restart
    exit 1
  fi
  if ! tofu apply -input=false -auto-approve -no-color; then
    [ -n "$old_type" ] || exit 1
    echo "the new server type could not be placed; rebuilding as the $old_type the server was" >&2
    tofu apply -input=false -auto-approve -no-color -var "server_type=$old_type"
  fi
fi
device=$(tofu output -raw backup_device)

step "wait for the new server's first boot"
ssh-keygen -R "$ip" >/dev/null 2>&1 || true
for _ in $(seq 60); do
  ssh -o StrictHostKeyChecking=accept-new -o ConnectTimeout=5 -o BatchMode=yes "$host" true 2>/dev/null && break
  sleep 5
done
# Short sessions, since the bootstrap turns the firewall on mid-way.
for _ in $(seq 60); do
  case $(ssh -o ConnectTimeout=5 -o BatchMode=yes "$host" 'cloud-init status' 2>/dev/null) in
  *done*) break ;;
  *error*)
    echo "the new server's bootstrap failed: see /var/log/cloud-init-output.log there" >&2
    exit 1
    ;;
  esac
  sleep 10
done
ssh "$host" 'cloud-init status' | grep -q 'status: done'

step "put /srv/secrets back from the volume"
ssh "$host" "sudo install -d /mnt/backup && sudo mount $device /mnt/backup"
# On --resume the newest day on the volume is the backup the failed run took.
[ -n "$day" ] || day=$(ssh "$host" 'sudo ls -1 /mnt/backup' | { grep -E '^[0-9]{4}-[0-9]{2}-[0-9]{2}$' || true; } | sort | tail -1)
[ -n "$day" ] || { echo "no backup day on the volume" >&2; exit 1; }
echo "restoring the backup of $day"
ssh "$host" "sudo tar -C /srv -xzf /mnt/backup/$day/secrets.tar.gz && sudo chown -R $user:$user /srv/secrets"
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
