#!/usr/bin/env bash
# Restores one night's backup of a service, decrypted on this machine with
# the secret half of the backup key, read over SSH from the server's settings
# file into a temporary file. By default it loads the database into a scratch database
# next to the live one, reports its tables and rows and drops it, to prove
# the backup works. --live replaces the service's database, with the service
# stopped meanwhile.
#
#   ./backup/restore.sh deploy@SERVER                                  list the days stored
#   ./backup/restore.sh deploy@SERVER DAY trader|notifier|app [--live]
set -euo pipefail
host=${1:?usage: ./backup/restore.sh deploy@SERVER [DAY SERVICE [--live]]}
cd "$(dirname "$0")"
if [ $# -eq 1 ]; then
  ssh "$host" /srv/backup/backup.sh days
  exit
fi
day=${2:?day} svc=${3:?service}
case $svc in
trader | notifier | app) ;;
*)
  echo "the service is trader, notifier or app" >&2
  exit 1
  ;;
esac
identity=$(mktemp)
trap 'rm -f "$identity"' EXIT
ssh "$host" "sed -n '/^\[backup\]/,/^\[/s/^BACKUP_AGE_IDENTITY=//p' /srv/secrets/candylario.env" >"$identity"
[ -s "$identity" ] || {
  echo "no BACKUP_AGE_IDENTITY under [backup] in the server's settings file" >&2
  exit 1
}
fetch() { ssh "$host" "/srv/backup/backup.sh cat $day $1" | age -d -i "$identity"; }
db=restore_check
if [ "${4:-}" = --live ]; then
  db=$svc
  ssh "$host" "cd /srv/$svc && docker compose stop $svc"
fi
ssh "$host" "cd /srv/$svc && docker compose exec -T postgres sh -c 'dropdb -U $svc --if-exists restore_check && { [ $db = $svc ] || createdb -U $svc restore_check; }'"
fetch "$svc.sql.gz.age" | gunzip | ssh "$host" "cd /srv/$svc && docker compose exec -T postgres psql -q -v ON_ERROR_STOP=1 -U $svc -d $db" >/dev/null
if [ "$db" = restore_check ]; then
  ssh "$host" "cd /srv/$svc && docker compose exec -T postgres psql -q -U $svc -d restore_check -c \"do \\\$\\\$ declare t record; n bigint; rows bigint := 0; tables int := 0; begin for t in select tablename from pg_tables where schemaname = 'public' loop execute format('select count(*) from %I', t.tablename) into n; rows := rows + n; tables := tables + 1; end loop; raise notice '% tables, % rows', tables, rows; end \\\$\\\$\" 2>&1 | sed 's/^NOTICE: */$svc on $day: /' && docker compose exec -T postgres dropdb -U $svc restore_check"
  exit
fi
ssh "$host" "cd /srv/$svc && docker compose start $svc"
echo "restored $svc from $day"
