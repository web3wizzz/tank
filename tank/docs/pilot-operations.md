# Phase 2 pilot operations

These are configurable hosted-pilot templates, not a deployed application or
storage pilot. TankRegistry is already deployed and registered on Arc Testnet;
see [the acceptance evidence](arc-testnet-acceptance.md). Frontend/coordinator
HTTPS hosts, independent storage hosts, operator access, pilot users, and owner
decisions remain pending. Do not use placeholders as real hosts, keys, or service
configuration.

Start with the [independent-host handoff](independent-host-pilot-handoff.md) for
the minimal five-machine topology, private input inventory, exact deployment and
acceptance commands, sizing assumptions and owner decisions. It preserves the
existing registry/signing lane and leaves remote failure execution disabled.

Arc Testnet is the public registry target. Mainnet remains outside this pilot
scope. The registry deployment is separate from frontend/coordinator hosting
and independent storage machines; Arc does not store file shards.

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

### Reproducible release builds

Build from a recorded full Git commit on a clean release worktree. Record the
operating system/architecture and Go, Node/npm, and Arc Foundry versions. Verify
Go module checksums, use each checked-in npm lockfile with `npm ci`, and retain
build logs and binary/artifact checksums in the operator's private release record.
Never include runtime configuration, credential files, databases, signed
journals, or user recovery keys in a build artifact.

From the repository root:

```bash
git status --short
git rev-parse HEAD
go version
node --version
npm --version
cd tank
go mod verify
go build -trimpath -o bin/ ./cmd/...
cd sdk-ts
npm ci
npm test
npm run check
cd ../frontend
npm ci
npm test
npm run lint
npm run build
```

The frontend build needs its selected canonical `TANK_SITE_URL` at build time.
Keep that public origin stable; load the session secret from
`/etc/tank/frontend.conf` only at runtime. Pin the source commit and lockfiles
when copying a release to a host. This makes build inputs reviewable; bitwise
identical artifacts still need confirmation from the recorded environment.

For selected hosts, `deploy/pilot/systemd/` defines coordinator, node, frontend,
and bounded Arc worker lifecycle units. `deploy/pilot/*.conf.example` provides
editable configuration templates.
The same node unit/template runs on each of four machines, with its own directory,
HTTPS endpoint, and credential. Installation/provisioning is not automated here.

Install the selected units and configuration only after host inventory, paths,
ownership, firewall rules, and certificate renewal are reviewed. Validate units
before starting them:

```bash
sudo systemd-analyze verify /etc/systemd/system/tank-*.service /etc/systemd/system/tank-*.timer
sudo systemctl daemon-reload
sudo systemctl enable --now tank-node.service       # each node host
sudo systemctl enable --now tank-coordinator.service
sudo systemctl enable --now tank-frontend.service
```

The Arc registration timer is separate and disabled by default. Install
`deploy/arc-testnet/testnet.conf.example` as a private worker environment file,
replace only its documented placeholders locally, point its database path at the
coordinator's existing journal, and verify the same account, registry, 65 Gwei
ceiling, per-registration 0.008 USDC ceiling, and aggregate 0.10 USDC ledger. Do
not duplicate or reset that journal. Run the existing non-broadcast preflight
first; then an authorized operator can install/enable the timer on the one worker
host. Never start a second worker/account lane.

Use `systemctl status` and authenticated operational reports to check lifecycle.
The worker is a bounded oneshot on a five-minute timer; retries keep their signed
transaction bytes and permanent spend reservations. A failed worker pass does not
authorize raising fee or total-spend caps.

Create the `tank` service user and private `/etc/tank` configuration out of band.
Service config can be root:tank mode0640; the coordinator's credential JSON must
be readable by `tank` and mode0600 or0400. It maps each canonical URL (no trailing
slash) to its unique private node token, in the configured placement order.
`TANK_NODE_CREDENTIALS_FILE` selects this map; `TANK_NODE_TOKEN` remains a legacy
shared local-mode fallback. Never copy all node credentials onto a storage host.
Keep administrator credentials on the coordinator/operator tooling; frontend
config contains only its exact HTTPS origin, coordinator URL, persistent session
secret, and frontend limits. The registration worker uses the same coordinator
database for its nonce lane and permanent spend ledger.

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

### Local reference measurement (2026-10-13)

One `TANK_E2E_MEASURE=1 npm run test:pilot` run on Linux 6.8 x86_64, 2 reported
CPUs, Node 24.21.0, Go 1.27.1, Playwright 1.64.0, and the local four-process
storage stack produced:

| Sample | Measured result |
| --- | ---: |
| Synthetic PDF plaintext | 591 bytes |
| Encrypted upload envelope | 673 bytes |
| Browser encryption step | 290.11 ms |
| Upload and service acknowledgement | 126.36 ms |
| Retrieve, verify, decrypt, and download | 67.74 ms |
| Complete local pilot drill, including builds | 28.90 s |

These are single-sample loopback measurements from the local emulation. They
provide a repeatable baseline for the existing 8 KiB test configuration, not a
capacity or latency claim for a remote host. No p95, sustained throughput, host
RSS/disk use, TLS renewal, independent failure-domain, or off-machine restore-time
measurement exists yet. Repeat with documented file-size and concurrency sets
after real pilot hosts are selected. Set `TANK_E2E_MEASURE=1` on the local pilot
command to print synthetic byte counts and phase timings; it prints no file data,
credentials, or recovery key.

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

The signing/restore safeguards now have isolated local automation and canonical
chain validator coverage: [failure and restore safety](hosted-restore-safety.md).
Use `--dry-run` on the hosted preflight for its plan; remote mutation remains
disabled pending explicit hosts/operator access and activation/browser integration.

From `tank/frontend/`, after building the linked SDK/frontend and installing
Chromium, run `npm run test:pilot`. Run the local pilot drill first; it uses
synthetic users/data and owned temporary processes only. Record it as local
emulation. The hosted acceptance runner requires an explicit execute flag and
separately configured independent hosts; its dry run does not stop services. The
eventual real-host drill must
stop one entire active storage machine, restore the encrypted PDF with its
original filename and exact bytes, observe automatic repair onto the independent
spare, restart/restore coordinator metadata, and confirm another user remains
isolated. Record the host identities/failure domains, commit, timestamps, and
checksums without secrets. Do not mark the real pilot complete from loopback tests.
