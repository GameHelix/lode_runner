#!/usr/bin/env bash
# Copies every Candylario database, and the settings in /srv/secrets that a
# rebuilt server needs, onto the backup disk mounted at BACKUP_DIR, under the
# day's date, keeping 30 days. When it fails, the admin gets an email through
# the app's Mailjet keys.
#
#   ./backup.sh                 back up now; candylario-backup.timer runs this nightly
#   ./backup.sh days            list the days stored
#   ./backup.sh cat DAY FILE    print one stored file
set -euo pipefail
cd "$(dirname "$0")"
set -a
. ./.env
set +a

case ${1:-} in
days)
  for d in "$BACKUP_DIR"/[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]; do
    if [ -d "$d" ]; then basename "$d"; fi
  done
  exit
  ;;
cat)
  cat "$BACKUP_DIR/${2:?day}/${3:?file}"
  exit
  ;;
esac

day=$(date -u +%F)
out="$BACKUP_DIR/.$day"
trap 'rm -rf "$out"' EXIT

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

mountpoint -q "$BACKUP_DIR"
umask 077
rm -rf "$out"
mkdir "$out"
for svc in trader notifier app; do
  (cd "/srv/$svc" && docker compose exec -T postgres pg_dump -U "$svc" -d "$svc" --clean --if-exists) | gzip >"$out/$svc.sql.gz"
done
tar -C /srv -czf "$out/secrets.tar.gz" secrets
rm -rf "${BACKUP_DIR:?}/$day"
mv "$out" "$BACKUP_DIR/$day"
find "$BACKUP_DIR" -mindepth 1 -maxdepth 1 -type d -name '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]' -mtime +30 -exec rm -rf {} +
echo "backed up $day: $(cd "$BACKUP_DIR/$day" && ls | tr '\n' ' ')"
