#!/usr/bin/env bash
# Runs OpenTofu here on the state, the variables and the Hetzner token kept
# on the server in /srv/secrets, so none of them stays on this machine: the
# state and variables are fetched for the run and deleted after it, and the
# state goes back to the server first whenever the run changed it.
#
#   ./tofu.sh deploy@SERVER plan|apply|... [ARGS]
# shellcheck disable=SC2029 # the paths are meant to expand here, before ssh.
set -euo pipefail
host=${1:?usage: ./tofu.sh deploy@SERVER COMMAND [ARGS]}
shift
cd "$(dirname "$0")"
remote=/srv/secrets/infra
scp -q "$host:$remote/terraform.tfstate" "$host:$remote/terraform.tfvars" .
before=$(shasum terraform.tfstate)
finish() {
  if [ "$(shasum terraform.tfstate)" != "$before" ]; then
    if ! scp -q terraform.tfstate "$host:$remote/terraform.tfstate"; then
      echo "the new state could not go back to $host; it stays in $PWD/terraform.tfstate" >&2
      rm -f terraform.tfvars terraform.tfstate.backup
      return
    fi
  fi
  rm -f terraform.tfstate terraform.tfstate.backup terraform.tfvars
}
trap finish EXIT
# The infra workflow passes the repository secret; the server's copy is the fallback.
HCLOUD_TOKEN=${HCLOUD_TOKEN:-$(ssh "$host" "sed -n 's/^HCLOUD_TOKEN=//p' /srv/secrets/candylario.env")}
export HCLOUD_TOKEN
tofu init -input=false >/dev/null
tofu "$@"
