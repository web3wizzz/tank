#!/usr/bin/env bash
# Disposable CI fixture only. Never use real pilot volumes or signing custody.
set -euo pipefail
set +x
cd "$(dirname "$0")/.."
identity="tank-ci-${GITHUB_RUN_ID:?CI run ID required}-${GITHUB_RUN_ATTEMPT:-1}"
[[ "$identity" =~ ^tank-ci-[0-9]+-[0-9]+$ ]] || exit 1
# Do not reuse another run's project/volumes, including a repeated local CI ID.
project="$identity-$(python3 -c 'import secrets; print(secrets.token_hex(6))')"
printf 'CI-owned container project: %s\n' "$project"
umask 077
fixture=$(mktemp -d)
envfile="$fixture/local.env"
python3 - "$envfile" "$fixture" <<'PY'
import json, os, secrets, sys
from pathlib import Path
with open(sys.argv[1], 'x') as f:
    for name in ('TANK_CONTAINER_NODE_TOKEN', 'TANK_CONTAINER_ADMIN_TOKEN', 'TANK_CONTAINER_SESSION_SECRET'):
        f.write(name + '=' + secrets.token_hex(32) + '\n')
# Explicitly simulated domains exercise the real startup map in the image.
root = Path(sys.argv[2]).resolve()
domains = root / 'node-domains.json'
with domains.open('x') as f:
    json.dump({f'http://127.0.0.1:{9100 + index}': f'local-fixture-{index}'
               for index in range(1, 5)}, f)
os.chmod(domains, 0o644)  # Nonsecret labels; runtime UID 10001 reads this mount.
with (root / 'domains.compose.json').open('x') as f:
    json.dump({'services': {'coordinator': {
        'environment': {'TANK_REQUIRE_NODE_FAILURE_DOMAINS': 'true',
                        'TANK_NODE_FAILURE_DOMAINS_FILE': '/config/node-domains.json'},
        'volumes': [{'type': 'bind', 'source': str(domains),
                     'target': '/config/node-domains.json', 'read_only': True}]
    }}}, f)
PY
# Readiness/stop timeouts do not bound a stalled Docker daemon. Bound every
# external acceptance operation, including cleanup, and force termination if TERM
# is ignored. This cannot guarantee daemon-side cleanup after client termination.
bounded() {
  local duration="$1"
  shift
  timeout --kill-after=5s "$duration" "$@"
}
compose=(docker compose --env-file "$envfile" -p "$project" -f compose.yaml -f compose.registration.yaml -f "$fixture/domains.compose.json")
cleanup() {
  result=$?
  if ((result != 0)); then
    # Only these synthetic fixture services; Anvil's key banner is redirected.
    bounded 15s "${compose[@]}" logs --no-color --tail 40 local-chain registration-worker || true
  fi
  # The generated project name exclusively owns these ephemeral CI volumes.
  bounded 15s docker rm -f "${project}-runtime-inventory" >/dev/null 2>&1 || true
  if ! bounded 60s "${compose[@]}" down --volumes --timeout 30 >/dev/null 2>&1; then
    echo "CI-owned container cleanup failed for project $project; inspect only this project before retrying." >&2
    result=1
  fi
  rm -rf "$fixture"
  exit "$result"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
bounded 30s "${compose[@]}" config --quiet
bounded 18m "${compose[@]}" build coordinator frontend local-chain
bounded 45s docker run --rm --name "${project}-runtime-inventory" --network none --read-only \
  --cap-drop ALL --security-opt no-new-privileges tank-local-frontend node -e '
    const fs = require("node:fs");
    for (const path of ["/usr/local/bin/npm", "/usr/local/bin/npx", "/usr/local/bin/corepack", "/usr/local/bin/yarn", "/usr/local/bin/yarnpkg", "/usr/local/bin/pnpm", "/usr/local/bin/pnpx"]) {
      try { fs.lstatSync(path); } catch (error) { if (error.code === "ENOENT") continue; throw error; }
      throw new Error("Unnecessary package manager remains in runtime image");
    }
    if (fs.existsSync("/usr/local/lib/node_modules") && fs.readdirSync("/usr/local/lib/node_modules").length) throw new Error("Unnecessary global tooling remains in runtime image");
    if (fs.readdirSync("/opt").some(name => name.startsWith("yarn-"))) throw new Error("Inherited Yarn payload remains in runtime image");
    console.log("PASS: final frontend runtime image excludes inherited package managers.");
  '

bounded 240s "${compose[@]}" up -d --wait --wait-timeout 180
# Freeze processing before creating the browser upload. The one-shot enqueue
# command below takes the same process lock and never contacts/signs on a chain.
bounded 45s "${compose[@]}" stop --timeout 30 registration-worker
for user in a b; do
  bounded 45s "${compose[@]}" exec -T coordinator tank-access create --db /data/tank.sqlite \
    --label "Container CI $user" --out "/data/acceptance/user-$user.json" >/dev/null
  bounded 30s "${compose[@]}" cp "coordinator:/data/acceptance/user-$user.json" "$fixture/user-$user.json" >/dev/null
  chmod 0600 "$fixture/user-$user.json"
done
(
  cd frontend
  bounded 4m env -u TANK_E2E_UPSTREAM_URL -u TANK_E2E_CODESPACES_AUTH -u TANK_E2E_PILOT \
    -u TANK_E2E_ACCESS_TOOL -u TANK_E2E_REPLACEMENT_FILE -u TANK_E2E_PAGINATION \
    -u TANK_E2E_MAX_FILE_BYTES -u TANK_E2E_EXPECT_REGISTRATION_STATUS -u TANK_E2E_MEASURE \
    TANK_FRONTEND_ORIGIN=http://localhost:3330 \
    TANK_E2E_CREDENTIAL_FILE="$fixture/user-a.json" \
    TANK_E2E_OTHER_CREDENTIAL_FILE="$fixture/user-b.json" npm run test:browser
)
bounded 45s "${compose[@]}" run --rm --no-deps -T registration-worker tank-registration-worker --enqueue-only
bounded 150s python3 scripts/container-state-check.py "$fixture" queued
bounded 60s "${compose[@]}" down --timeout 30
bounded 240s "${compose[@]}" up -d --wait --wait-timeout 180
bounded 150s python3 scripts/container-state-check.py "$fixture" snapshot
nonce_before=$(bounded 15s "${compose[@]}" exec -T local-chain arc-cast nonce \
  0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266 --rpc-url http://127.0.0.1:8545)
[[ "$nonce_before" =~ ^[0-9]+$ ]] || exit 1
bounded 60s "${compose[@]}" down --timeout 30
bounded 240s "${compose[@]}" up -d --wait --wait-timeout 180
bounded 150s python3 scripts/container-state-check.py "$fixture" verify
nonce_after=$(bounded 15s "${compose[@]}" exec -T local-chain arc-cast nonce \
  0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266 --rpc-url http://127.0.0.1:8545)
[[ "$nonce_after" == "$nonce_before" ]] || { echo 'Restart unexpectedly sent another local transaction.' >&2; exit 1; }
echo 'PASS: disposable container encryption, queued registration recovery, stable nonce/transaction, and user isolation.'
