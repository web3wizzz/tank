# Phase 2 pilot operations

These are configurable preparation templates, not a deployed pilot. Actual hosts,
RPC provider, signer, users, funding, and remote deployment approval are pending.
Do not use placeholders as real hosts, keys, or service configuration.

## Topology and capacity

Use three active storage machines and a fourth unused spare, each on independent
physical failure domains. Separate ports/containers on one host do not establish
machine-loss durability. The coordinator stores metadata separately; the frontend
can run with it or on another selected host. Verify host/provider failure domains
in a private operator inventory before claiming a distributed pilot.

Use a supported Linux/systemd host and persistent local storage, not an ephemeral
container filesystem. The checked-in initial budgets are 16 MiB uploads, four
coordinator requests, 1 GiB logical bytes/user, 10 GiB logical total, and 10 GiB
retained bytes/node. Tune using measured pilot load. Reed–Solomon adds 50% shard
bytes before metadata and filesystem overhead; each segment places two of six
shards on each active node. Spare repair and retained partial writes need capacity.

A starting validation budget is 2 CPU cores and 2 GiB RAM per service host, with
at least 20 GiB free persistent space per node for the default 10 GiB cap. This is
an unmeasured pilot sizing assumption, not a minimum or performance guarantee.
Measure peak RSS, CPU, latency, bandwidth, free disk/inodes, and restore times
before onboarding users. Frontend builds and Go builds can run on a separate
build machine. Choose more capacity or lower request/byte limits based on results.

## Configuration and secret custody

Build the pinned repository versions locally with `go build -o bin/ ./cmd/...`.
For the future selected hosts, `deploy/pilot/systemd/` defines coordinator/node
units and `deploy/pilot/*.conf.example` provides editable configuration templates.
The same node unit/template runs on each of four machines, with its own directory,
HTTPS endpoint, and credential. Installation/provisioning is not automated here.

Create the `tank` service user and private `/etc/tank` configuration out of band.
Service config can be root:tank mode0640; the coordinator's credential JSON must
be readable by `tank` and mode0600 or0400. It maps each canonical URL (no trailing
slash) to its unique private node token, in the configured placement order.
`TANK_NODE_CREDENTIALS_FILE` selects this map; `TANK_NODE_TOKEN` remains a legacy
shared local-mode fallback. Never copy all node credentials onto a storage host.
Keep administrator credentials on the coordinator/operator tooling; frontend
config contains only its persistent session secret and coordinator URL.

Generate secrets privately, without shell tracing or printing them. The actual
`.conf` files, credential maps, environment, TLS private keys, snapshots, logs,
and stored shards are runtime state. Keep them outside the repository; only the
`.example` templates belong in commits. Restrict backups to authorized operators.
Issue individual user credentials locally with `tank-access`; record principal
and credential IDs without tokens. Renewal preserves file access. Revoke leaked
credentials and reconcile revocations after metadata restore.

## Ports, TLS, and host security

Go services bind loopback: coordinator8080, each node9101, frontend3000.
Terminate HTTPS on a selected reverse proxy on443; `Caddyfile.example` shows the
upstreams, with real DNS/TLS configuration left to the operator. Certificates must
validate with the coordinator's trust store. No skip-verification mode is added.
Remote node HTTP is rejected; only loopback HTTP is allowed. Redirects are refused
so a node cannot redirect bearer credentials elsewhere.

Limit node443 ingress to the selected coordinator/monitoring hosts. Limit
coordinator443 to the frontend/operator network; keep raw backends and SQLite off
public interfaces. The public-facing frontend uses its exact HTTPS origin, secure
HttpOnly cookies, and browser encryption. Set a stable session secret across
restarts. Configure firewalls, time synchronization, TLS renewal, and private log
rotation before any real deployment. Do not publish private Codespaces/backend
ports as substitutes for selected pilot infrastructure.

## Monitoring, backups, and retention

Use the [operational reports](monitoring.md), host disk/process checks, and
synthetic encrypted retrieval rather than liveness alone. Alert on unavailable
nodes, exhausted capacity, rising repair backlog, failed/stale audits, failed
registration, stale backup age, and TLS expiry.

Use [private online snapshots and restore drills](backup-restore.md). Store node
snapshots and consistent metadata off their source machines in private custody.
Original browser recovery keys remain user-owned. Record actual recovery-point
and restore-time measurements; no automatic data deletion is enabled.

Before onboarding pilot users, fill in the retention agreement:

| Decision | Operator must specify |
| --- | --- |
| Retention term | Explicit start/end dates or renewable duration |
| Funding | Who funds host, bandwidth, metadata, backups, and repair capacity |
| User obligations | Keep original copies and private recovery keys; respect quota |
| Operator exit | Notice period, handover, shard transfer, and funded replacement |
| Capacity failure | Admission policy, incident contacts, and recovery priorities |
| End of pilot | Export window, user notice, and explicit future deletion policy |

There is no funded perpetual-retention promise, operator payment, or automatic
penalty. Do not advertise “Retrieve Forever” as a guarantee. Storage verification,
incentives, larger/resumable files, sharing, and rollup data availability remain
separate later scopes.

## Acceptance evidence

Run the local pilot drill first; it uses synthetic users/data and owned temporary
processes only. Record it as local emulation. The eventual real-host drill must
stop one entire active storage machine, restore the encrypted PDF with its
original filename and exact bytes, observe automatic repair onto the independent
spare, restart/restore coordinator metadata, and confirm another user remains
isolated. Record the host identities/failure domains, commit, timestamps, and
checksums without secrets. Do not mark the real pilot complete from loopback tests.
