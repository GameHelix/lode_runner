#!/usr/bin/env bash
# Gives GitHub Actions its own SSH key to the server, for deploys and
# OpenTofu runs: the private half becomes the repository secret
# DEPLOY_SSH_KEY, the public half joins the deploy user's authorized_keys,
# the server's host key becomes DEPLOY_KNOWN_HOSTS, and SERVER_IP, set
# last, switches deploys on. No copy of the key stays on this machine. Run
# it again to replace the key; remove the old line from authorized_keys.
#
#   ./infra/actions-key.sh [deploy@SERVER]
set -euo pipefail
host=${1:-deploy@91.99.222.102}
repo=candylario/candylario
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
ssh-keygen -q -t ed25519 -N '' -C "candylario github actions $(date -u +%F)" -f "$work/key"
ssh-keyscan -t ed25519 "${host#*@}" 2>/dev/null >"$work/known_hosts"
[ -s "$work/known_hosts" ] || {
  echo "no host key from ${host#*@}" >&2
  exit 1
}
ssh "$host" 'cat >>~/.ssh/authorized_keys' <"$work/key.pub"
gh secret set DEPLOY_SSH_KEY -R "$repo" <"$work/key"
gh secret set DEPLOY_KNOWN_HOSTS -R "$repo" <"$work/known_hosts"
gh variable set SERVER_IP -R "$repo" -b "${host#*@}"
echo "GitHub Actions now deploys to ${host#*@}."
