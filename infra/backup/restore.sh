#!/usr/bin/env bash
# Restores one night's backup of a service from the server's backup disk, on
# the server. By default it loads the database into a scratch database next
# to the live one, reports its tables and rows and drops it, to prove the
# backup works. --live replaces the service's database, with the service
# stopped meanwhile.
#
#   ./backup/restore.sh deploy@SERVER                                  list the days stored
#   ./backup/restore.sh deploy@SERVER DAY trader|notifier|app [--live]
set -euo pipefail
host=${1:?usage: ./backup/restore.sh deploy@SERVER [DAY SERVICE [--live]]}
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
db=restore_check
if [ "${4:-}" = --live ]; then
  db=$svc
fi
ssh "$host" bash -s -- "$day" "$svc" "$db" <<'RESTORE'
set -euo pipefail
day=$1 svc=$2 db=$3
cd "/srv/$svc"
pg() { docker compose exec -T postgres "$@"; }
if [ "$db" = "$svc" ]; then
  docker compose stop "$svc"
else
  pg dropdb -U "$svc" --if-exists restore_check
  pg createdb -U "$svc" restore_check
fi
/srv/backup/backup.sh cat "$day" "$svc.sql.gz" | gunzip | pg psql -q -v ON_ERROR_STOP=1 -U "$svc" -d "$db" >/dev/null
if [ "$db" = "$svc" ]; then
  docker compose start "$svc"
  echo "restored $svc from $day"
  exit
fi
pg psql -At -U "$svc" -d restore_check -c "
  select '$svc on $day: ' || count(*) || ' tables, ' ||
    coalesce(sum((xpath('/row/c/text()', query_to_xml(format('select count(*) as c from public.%I', table_name), false, true, '')))[1]::text::bigint), 0) || ' rows'
  from information_schema.tables where table_schema = 'public' and table_type = 'BASE TABLE'"
pg dropdb -U "$svc" restore_check
RESTORE
