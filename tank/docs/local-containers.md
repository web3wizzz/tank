# Local container packaging

This packages the existing coordinator, storage nodes, credential/backup tools and
working Next.js workspace. It is a **single-machine local simulation**, with three
active node containers and one spare. It supplies no independent-host durability,
TLS termination, signing worker, RPC, custody or Arc acceptance evidence. Existing
registry/account/journals and transaction budgets are untouched.

Prerequisites: Docker Engine with Compose, sufficient space for Go/npm build
layers, and Python 3 for local secret generation. Images use Go 1.26, Node 24 and
checked-in dependency locks and digest-pinned base images; record source commit
and built image IDs. Update base digests deliberately and revalidate. Builds need
access to public image/module/npm registries and the existing
font build dependency. No runtime secrets are build arguments.

The frontend runtime uses Next's traced standalone artifact instead of copying
the complete build workspace. Static/public assets are copied separately and the
generated `server.js` is the entrypoint. A build-time inventory rejects lint/test
tooling in the actual runtime artifact. See [dependency exposure](dependency-exposure.md)
for the unresolved braces development advisory and the limits of npm audit evidence.

## Start a separate local stack

From `tank/`, create a private environment once. Do not regenerate it while
reusing volumes: session and node/admin credentials must stay stable. The command
refuses to overwrite an existing file and prints no secret:

```bash
set -euo pipefail
set +x
umask 077
python3 - <<'PY'
from pathlib import Path
from secrets import token_hex
directory = Path.home() / '.config' / 'tank-local-containers'
directory.mkdir(parents=True, exist_ok=True, mode=0o700)
directory.chmod(0o700)
with (directory / 'local.env').open('x') as output:
    for name in ('TANK_CONTAINER_NODE_TOKEN', 'TANK_CONTAINER_ADMIN_TOKEN',
                 'TANK_CONTAINER_SESSION_SECRET'):
        output.write(f'{name}={token_hex(32)}\n')
PY
TANK_CONTAINER_ENV="$HOME/.config/tank-local-containers/local.env"
docker compose --env-file "$TANK_CONTAINER_ENV" -p tank-local config --quiet
docker compose --env-file "$TANK_CONTAINER_ENV" -p tank-local build coordinator frontend
docker compose --env-file "$TANK_CONTAINER_ENV" -p tank-local up -d --wait --wait-timeout 120
```

Open **http://localhost:3330**; localhost is required for browser Web Crypto.
`TANK_CONTAINER_PORT` optionally changes the published port and canonical/runtime
origin together; rebuild the frontend after changing it. Stop on a port conflict
instead of stopping an unrelated service. Only frontend port 3330 is published,
bound to host 127.0.0.1. Coordinator/node ports remain loopback inside the shared
container network namespace. The shared namespace is intentional: the node client
rejects non-loopback plain HTTP. See Docker's [service namespace configuration](https://docs.docker.com/reference/compose-file/services/).
If the coordinator container is replaced, recreate the whole local stack with
`up -d --force-recreate --wait --wait-timeout 120` so peers join the new namespace.

Named project volumes keep metadata (including user authorization), three active
shard roots and the spare root separate and persistent. Never mount the existing
real pilot database, shards, deployment journals or keystore into this fixture.
Go services run as UID/GID 10001; frontend runs as the image's `node` user. New
volumes inherit the image's private data directory. Existing external volumes
need a separately reviewed ownership/migration plan; no startup chown/reset runs.
Services have read-only roots, dropped capabilities, no privilege escalation,
bounded health checks, graceful stops and rotated container logs. Cache/tmp are
ephemeral. Health checks show liveness, not data recovery or chain confirmation.

This local compatibility configuration uses one freshly generated shared node
token. Independent-host deployment requires distinct per-node credentials and
valid HTTPS as described in the [pilot handoff](independent-host-pilot-handoff.md).
Container environment metadata is visible to Docker administrators; keep daemon
access restricted. Use `config --quiet`, never print rendered configuration or
container environment. The default-deny [build context](../.dockerignore) includes
only selected source/build inputs and excludes local data, secrets and generated
dependencies; Docker documents [ignore rule ordering](https://docs.docker.com/build/concepts/context/).

## Create local access and run one encrypted smoke

Create two synthetic users in this stack's database, with fresh output paths.
Existing files are never replaced. Copy only these synthetic credentials to a
private directory on the operator machine; never paste them into chat:

```bash
set -euo pipefail
set +x
umask 077
TANK_CONTAINER_ENV="$HOME/.config/tank-local-containers/local.env"
TANK_CONTAINER_CHECK_DIR=$(mktemp -d)
docker compose --env-file "$TANK_CONTAINER_ENV" -p tank-local exec -T coordinator \
  tank-access create --db /data/tank.sqlite --label 'Container check A' --out /data/acceptance/user-a.json
docker compose --env-file "$TANK_CONTAINER_ENV" -p tank-local exec -T coordinator \
  tank-access create --db /data/tank.sqlite --label 'Container check B' --out /data/acceptance/user-b.json
docker compose --env-file "$TANK_CONTAINER_ENV" -p tank-local cp coordinator:/data/acceptance/user-a.json "$TANK_CONTAINER_CHECK_DIR/user-a.json"
docker compose --env-file "$TANK_CONTAINER_ENV" -p tank-local cp coordinator:/data/acceptance/user-b.json "$TANK_CONTAINER_CHECK_DIR/user-b.json"
chmod 0600 "$TANK_CONTAINER_CHECK_DIR/"*.json
cd frontend
# On a fresh acceptance machine, first build the linked SDK and install dependencies:
# npm --prefix ../sdk-ts ci && npm --prefix ../sdk-ts run build && npm ci
# npx playwright install chromium
env -u TANK_E2E_UPSTREAM_URL -u TANK_E2E_CODESPACES_AUTH -u TANK_E2E_PILOT \
  -u TANK_E2E_ACCESS_TOOL -u TANK_E2E_REPLACEMENT_FILE -u TANK_E2E_PAGINATION \
  -u TANK_E2E_MAX_FILE_BYTES -u TANK_E2E_EXPECT_REGISTRATION_STATUS -u TANK_E2E_MEASURE \
  TANK_FRONTEND_ORIGIN=http://localhost:3330 \
  TANK_E2E_CREDENTIAL_FILE="$TANK_CONTAINER_CHECK_DIR/user-a.json" \
  TANK_E2E_OTHER_CREDENTIAL_FILE="$TANK_CONTAINER_CHECK_DIR/user-b.json" npm run test:browser
```

For a changed port, substitute the exact configured localhost origin. This reuses
the real browser smoke: one synthetic encrypted upload, ciphertext hash/envelope
checks, exact decrypted bytes/Unicode filename, downloaded-key import, authenticated
registration status, and cross-user retrieval/registration denial. Without a chain
worker, status is `not_queued`; no chain success is asserted. Recovery-key material
is temporary browser/test data and is never logged or added to the image.
The tool creates expiring credentials; use `tank-access issue` for renewal without
changing principal ownership. Repeated runs require new credential output paths
or reuse existing synthetic credentials; do not reset the database to retry.

## Stop and preserve

From `tank/`, with the same private environment/project:

```bash
docker compose --env-file "$TANK_CONTAINER_ENV" -p tank-local down --timeout 30
```

Volumes remain intact. Do not add `--volumes`, run a volume prune, or delete private
data to make verification pass. Resume with the same environment and `up` command.
For a metadata snapshot, use `tank-backup` inside the coordinator with a **new**
`/data/` destination and privately copy it off-machine. An online snapshot is not
a signing/restore permit; see [backup safety](backup-restore.md) and
[replicated checkpoints](signing-state-recovery.md). Containerized hosted
signing/restore orchestration remains a separate integration gate. Disposable
local container CI is described below.


## Opt-in local registration fixture

The base simulation remains chainless. To exercise queued registration on an
isolated development chain, use the same private environment and project name
for both commands:

```bash
docker compose --env-file /private/tank-local.env -p tank-local -f compose.yaml -f compose.registration.yaml up --build -d --wait --wait-timeout 180
docker compose --env-file /private/tank-local.env -p tank-local -f compose.yaml -f compose.registration.yaml down --timeout 30
```

The overlay fixes chain 31337, a known development registrant and the expected
fixture registry. Anvil listens only inside the coordinator network namespace;
no RPC port is published. The worker shares the local metadata volume and queues
from the coordinator. Its stable database-adjacent process lock fences the whole local worker;
registration leases also protect queued persistence. The database must already
be initialized and its parent must be private and owned by the worker UID. Contract bytecode is verified before fixture readiness;
chain state persists in its own named volume. Missing registry with a changed
nonce fails rather than resetting history. Never mount public custody here.

For local maintenance, `tank-registration-worker --enqueue-only` reconciles stored
manifests into pending jobs under the same exclusive worker lock, with a 30-second
deadline and no RPC/signing/submission. Stop the normal worker before running it.
It requires the same initialized database and explicit local-chain configuration;
unknown flags, public RPC endpoints, concurrent workers and restore-candidate
gates are rejected. It does not authorize public Arc operations or recovery.

Public Arc signing is not supported by this development overlay. It remains an
explicit separately authorized hosted-worker operation using existing signer
custody, authoritative journal, canonical proof and fee limits. No Arc deployment,
public RPC or real fee expenditure occurs in container CI. Do not change only the
RPC URL to repurpose this fixture as a public signer.

The Containers workflow builds all three images, checks readiness, imports a
browser recovery key, verifies encrypted exact retrieval and cross-user denial,
stops the worker before upload and durably queues jobs without contacting the
chain. It verifies pending jobs with zero attempts, restarts the same CI volumes,
waits for registration, and restarts again to compare files, transaction hashes,
attempts and the local account nonce. Each run uses a random project suffix and
bounded build/browser/readiness/verification/cleanup waits. Cleanup removes only
that generated fixture's containers/volumes and reports failure. This is local
container evidence, not independent-host acceptance.
