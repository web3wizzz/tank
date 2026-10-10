#!/usr/bin/env bash
# Source from the local launchers. Existing settings and credentials are retained.
set -euo pipefail
umask 077

if [[ -L .env.tank-local ]]; then
  echo "Refusing a symlink for .env.tank-local." >&2
  exit 1
fi

# Lock initialization so simultaneous storage/frontend starts share one set of keys.
exec 9>>.env.tank-local
flock 9
chmod 600 .env.tank-local
set -a
source .env.tank-local
set +a

for tank_setting in TANK_NODE_TOKEN TANK_API_TOKEN TANK_SESSION_SECRET; do
  if [[ -z "${!tank_setting:-}" ]]; then
    printf -v "$tank_setting" '%s' "$(openssl rand -hex 32)"
    export "$tank_setting"
    # A leading newline also handles an existing file without a trailing newline.
    printf '\n%s=%s\n' "$tank_setting" "${!tank_setting}" >&9
  fi
done
unset tank_setting

# Persist the exact forwarded origin so restarts use the same allowlist.
if [[ -z "${TANK_FRONTEND_ORIGIN:-}" && -n "${CODESPACE_NAME:-}" ]]; then
  export TANK_FRONTEND_ORIGIN="https://${CODESPACE_NAME}-${PORT:-3000}.${GITHUB_CODESPACES_PORT_FORWARDING_DOMAIN:-app.github.dev}"
  printf '\nTANK_FRONTEND_ORIGIN=%q\n' "$TANK_FRONTEND_ORIGIN" >&9
fi
flock -u 9
exec 9>&-
