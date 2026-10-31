#!/usr/bin/env bash
# Prints one Candylario service's settings from the one file that holds them
# all, /srv/secrets/candylario.env on the server, read over SSH and never
# written to this machine. Keys before the first [section] are shared;
# [trader], [notifier] and [app] hold values for one service, such as its
# POSTGRES_PASSWORD, and [backup] the backup's. A service gets only the keys
# its .env.example lists, so the app never holds the engine's master key.
# deploy.sh ships this output.
#
#   ./env.sh trader|notifier|app|backup deploy@SERVER
set -euo pipefail
usage="usage: ./env.sh trader|notifier|app|backup deploy@SERVER"
svc=${1:-}
host=${2:?$usage}
cd "$(dirname "$0")"
case $svc in
trader | notifier | app) example=../$svc/.env.example ;;
backup) example=backup/.env.example ;;
*)
  echo "$usage" >&2
  exit 1
  ;;
esac
src=$(ssh "$host" cat /srv/secrets/candylario.env)
[ -n "$src" ] || {
  echo "/srv/secrets/candylario.env on $host is empty" >&2
  exit 1
}

awk -v svc="$svc" '
  /^[ \t]*(#|$)/ { next }
  FNR == NR && /^\[.*\]$/ { sect = substr($0, 2, length($0) - 2); next }
  { i = index($0, "="); if (!i) next; k = substr($0, 1, i - 1); v = substr($0, i + 1) }
  FNR == NR { if (sect == "") shared[k] = v; else if (sect == svc) own[k] = v; next }
  {
    if (k in own) v = own[k]; else if (k in shared) v = shared[k]
    print k "=" v
    if (v == "") printf "%s: %s is empty\n", svc, k >"/dev/stderr"
  }
' <(printf '%s\n' "$src") "$example"
