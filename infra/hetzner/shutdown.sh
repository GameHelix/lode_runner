#!/usr/bin/env bash
# Shuts Candylario down for good. It stops the three services, posts a
# closing note to the radar channel, destroys everything OpenTofu manages at
# Hetzner (the server, its address, the backup volume, the firewalls and the
# keys), deletes the snapshots and backups left in the project, and fails
# unless the project is then empty. The state and the variables come from
# the server, as in tofu.sh, and do not go back: the server is gone.
#
#   ./shutdown.sh deploy@SERVER --plan   # what the project holds and what would go
#   ./shutdown.sh deploy@SERVER          # shut down
set -euo pipefail
host=${1:?usage: ./shutdown.sh deploy@SERVER [--plan]}
mode=${2:-}
cd "$(dirname "$0")"
: "${HCLOUD_TOKEN:?shutdown.sh needs HCLOUD_TOKEN in the environment}"
export HCLOUD_TOKEN

api() { curl -fsS -H "Authorization: Bearer $HCLOUD_TOKEN" -H 'Content-Type: application/json' "https://api.hetzner.cloud/v1$1" "${@:2}"; }
step() { printf '\n== %s\n' "$*"; }

kinds=(servers volumes primary_ips floating_ips load_balancers networks firewalls ssh_keys certificates placement_groups)

# Prints what the project holds, kind by kind, and leaves the count in $left.
inventory() {
  local kind type line
  left=0
  for kind in "${kinds[@]}"; do
    line=$(api "/$kind?per_page=50" | jq -r --arg k "$kind" '.[$k] | "\(length) \(map(.name // "") | join(" "))"')
    printf '  %-16s %s\n' "$kind" "$line"
    left=$((left + ${line%% *}))
  done
  for type in snapshot backup; do
    line=$(api "/images?type=$type&per_page=50" | jq -r '.images | "\(length) \(map(.description // "") | join(", "))"')
    printf '  %-16s %s\n' "${type}s" "$line"
    left=$((left + ${line%% *}))
  done
}

remote=/srv/secrets/infra
scp -q "$host:$remote/terraform.tfstate" "$host:$remote/terraform.tfvars" .
# The variables never stay; the state stays only when a step fails, for the
# workflow to keep.
cleanup() {
  local status=$?
  rm -f terraform.tfvars
  if [ "$status" -eq 0 ]; then rm -f terraform.tfstate terraform.tfstate.backup; fi
}
trap cleanup EXIT
tofu init -input=false >/dev/null

if [ "$mode" = --plan ]; then
  step "what the project holds"
  inventory
  step "what OpenTofu would destroy"
  tofu plan -destroy -input=false -no-color
  exit 0
fi

step "stop the services, so the closing note is the channel's last post"
ssh "$host" 'for s in trader notifier app; do (cd "/srv/$s" && docker compose stop); done'

step "post the closing note to the radar channel"
ssh "$host" 'token=$(sed -n "s/^TELEGRAM_BOT_TOKEN=//p" /srv/notifier/.env)
  chat=$(sed -n "s/^RADAR_CHANNEL_ID=//p" /srv/notifier/.env)
  curl -fsS -o /dev/null --data-urlencode "chat_id=$chat" --data-urlencode "text@-" "https://api.telegram.org/bot$token/sendMessage"' <<'NOTE'
Candylario stops here. The radar is switched off today, 4 November 2026, and candy.surf closes with it; its accounts and data are deleted. Thank you for following.
NOTE
echo posted

step "lift the backup volume's delete protection"
tofu apply -target=hcloud_volume.backup -input=false -auto-approve -no-color

step "destroy the server, its address, the backup volume, the firewalls and the keys"
tofu destroy -input=false -auto-approve -no-color

step "delete the snapshots and backups left in the project"
for type in snapshot backup; do
  api "/images?type=$type&per_page=50" | jq -r '.images[].id' | while read -r id; do
    api "/images/$id/actions/change_protection" -X POST -d '{"delete":false}' >/dev/null
    api "/images/$id" -X DELETE >/dev/null
    echo "deleted $type $id"
  done
done

step "what the project holds now"
inventory
if [ "$left" -ne 0 ]; then
  echo "the project still holds $left things" >&2
  exit 1
fi
echo "the project is empty"
