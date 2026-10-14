#!/usr/bin/env bash
# Disposable CI fixture only. Never use real pilot volumes or signing custody.
set -euo pipefail
set +x
cd "$(dirname "$0")/.."
project="tank-ci-${GITHUB_RUN_ID:?CI run ID required}-${GITHUB_RUN_ATTEMPT:-1}"
[[ "$project" =~ ^tank-ci-[0-9]+-[0-9]+$ ]] || exit 1
umask 077
fixture=$(mktemp -d)
envfile="$fixture/local.env"
python3 - "$envfile" <<'PY'
import secrets, sys
with open(sys.argv[1], 'x') as f:
    for name in ('TANK_CONTAINER_NODE_TOKEN', 'TANK_CONTAINER_ADMIN_TOKEN', 'TANK_CONTAINER_SESSION_SECRET'):
        f.write(name + '=' + secrets.token_hex(32) + '\n')
PY
compose=(docker compose --env-file "$envfile" -p "$project" -f compose.yaml -f compose.registration.yaml)
cleanup() {
  result=$?
  if ((result != 0)); then
    # Only these synthetic fixture services; Anvil's key banner is redirected.
    "${compose[@]}" logs --no-color --tail 40 local-chain registration-worker || true
  fi
  # The generated project name exclusively owns these ephemeral CI volumes.
  "${compose[@]}" down --volumes --timeout 30 >/dev/null 2>&1 || true
  rm -rf "$fixture"
}
trap cleanup EXIT
"${compose[@]}" config --quiet
"${compose[@]}" build coordinator frontend local-chain
"${compose[@]}" up -d --wait --wait-timeout 180
for user in a b; do
  "${compose[@]}" exec -T coordinator tank-access create --db /data/tank.sqlite \
    --label "Container CI $user" --out "/data/acceptance/user-$user.json" >/dev/null
  "${compose[@]}" cp "coordinator:/data/acceptance/user-$user.json" "$fixture/user-$user.json" >/dev/null
  chmod 0600 "$fixture/user-$user.json"
done
(
  cd frontend
  env -u TANK_E2E_UPSTREAM_URL -u TANK_E2E_CODESPACES_AUTH -u TANK_E2E_PILOT \
    -u TANK_E2E_ACCESS_TOOL -u TANK_E2E_REPLACEMENT_FILE -u TANK_E2E_PAGINATION \
    -u TANK_E2E_MAX_FILE_BYTES -u TANK_E2E_EXPECT_REGISTRATION_STATUS -u TANK_E2E_MEASURE \
    TANK_FRONTEND_ORIGIN=http://localhost:3330 \
    TANK_E2E_CREDENTIAL_FILE="$fixture/user-a.json" \
    TANK_E2E_OTHER_CREDENTIAL_FILE="$fixture/user-b.json" npm run test:browser
)
python3 scripts/container-state-check.py "$fixture" snapshot
"${compose[@]}" down --timeout 30
"${compose[@]}" up -d --wait --wait-timeout 180
python3 scripts/container-state-check.py "$fixture" verify
echo 'PASS: disposable container encryption, local registration, restart persistence, and user isolation.'
