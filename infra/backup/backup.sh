#!/usr/bin/env bash
# Copies every Candylario database off this server:
# encrypted to an age key whose secret half never comes here, into S3-style
# object storage under the day's date, keeping 30 days. When it fails, the
# admin gets an email through the app's Mailjet keys.
#
#   ./backup.sh                 back up now; candylario-backup.timer runs this nightly
#   ./backup.sh days            list the days stored
#   ./backup.sh cat DAY FILE    print one stored file, still encrypted
set -euo pipefail
cd "$(dirname "$0")"
set -a
. ./.env
set +a
region=${BACKUP_S3_ENDPOINT#*://}
export RCLONE_CONFIG_STORE_TYPE=s3 RCLONE_CONFIG_STORE_PROVIDER=Other RCLONE_CONFIG_STORE_ACL=private \
  RCLONE_CONFIG_STORE_ENDPOINT="$BACKUP_S3_ENDPOINT" RCLONE_CONFIG_STORE_REGION="${region%%.*}" \
  RCLONE_CONFIG_STORE_ACCESS_KEY_ID="$BACKUP_S3_ACCESS_KEY" RCLONE_CONFIG_STORE_SECRET_ACCESS_KEY="$BACKUP_S3_SECRET_KEY" \
  RCLONE_CONFIG_STORE_NO_CHECK_BUCKET=true
store="store:$BACKUP_S3_BUCKET"

case ${1:-} in
days)
  rclone lsf --dirs-only "$store" | tr -d /
  exit
  ;;
cat)
  rclone cat "$store/${2:?day}/${3:?file}"
  exit
  ;;
esac

day=$(date -u +%F)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

alert() {
  local key secret to domain
  key=$(sed -n 's/^MAILJET_API_KEY=//p' /srv/app/.env)
  secret=$(sed -n 's/^MAILJET_SECRET_KEY=//p' /srv/app/.env)
  to=$(sed -n 's/^ADMIN_EMAILS=//p' /srv/app/.env | cut -d, -f1)
  domain=$(sed -n 's/^APP_DOMAIN=//p' /srv/app/.env)
  [ -n "$key" ] && [ -n "$to" ] && [ -n "$domain" ] || return 0
  curl -s -u "$key:$secret" -H 'Content-Type: application/json' https://api.mailjet.com/v3.1/send -d '{"Messages":[{
    "From":{"Email":"no-reply@'"$domain"'","Name":"Candylario"},"To":[{"Email":"'"$to"'"}],
    "Subject":"Candylario backup failed",
    "TextPart":"The backup of '"$day"' failed on '"$(hostname)"'. See journalctl -u candylario-backup there."}]}' >/dev/null || true
}
trap alert ERR

for svc in trader notifier app; do
  (cd "/srv/$svc" && docker compose exec -T postgres pg_dump -U "$svc" -d "$svc" --clean --if-exists) |
    gzip | age -r "$BACKUP_AGE_RECIPIENT" >"$tmp/$svc.sql.gz.age"
done
rclone copy "$tmp" "$store/$day"
rclone delete --min-age 30d "$store"
rclone rmdirs --leave-root "$store"
echo "backed up $day: $(cd "$tmp" && ls | tr '\n' ' ')"
