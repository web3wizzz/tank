# Coordinator availability assessment

Status: source-based assessment and local regression evidence, 2026-10-14.
No independent-host deployment, availability measurement or automatic failover is
established. The current pilot is one control host with frontend, coordinator,
local SQLite metadata and guarded signing worker; storage machines are separate.
See [disaster recovery design](disaster-recovery-design.md) for the missing durable
authority and custody fencing prerequisites.

## What currently recovers

`cmd/coordinator/main.go` owns one local `metadata.Store`, HTTP listener and
maintenance loop. SQLite uses WAL, FULL synchronization, a five-second busy timeout
and one connection per store. Systemd restarts a failed coordinator process after
five seconds; that is restart delay, not a measured outage bound. Startup, proxy,
SQLite recovery, readiness and retry time add to it. Graceful shutdown allows
15 seconds for HTTP; the unit allows 30 seconds to stop. Repair state and manifests
are durable; existing `TestRetrieveAfterNodeFailure` proves reopen/retrieval using
the same local database and local storage endpoints. It does not cover host/disk
loss or promotion to another machine.

Every file operation depends on coordinator metadata for manifest locations and
on authorization state for access. Shards on healthy nodes and an intact public
registry cannot serve the normal browser flow while the control host is down.
The local HTTP `/health` handler always returns `ok` when its process responds;
it does not test SQLite, nodes, revocations, signing readiness or backup freshness.
Authenticated `/ops/status` checks operational metadata/node conditions and upload
readiness. Synthetic encrypted upload/retrieval and denied cross-user requests are
needed for end-to-end evidence. Public liveness must not authorize failover.

| Failure | Current effect and recovery path | Evidence/limit |
| --- | --- | --- |
| Coordinator process exits; persistent disk intact | API unavailable until restart; reopen same state and drain durable maintenance work | Local reopen/process drills only; no measured host recovery bound |
| SQLite unavailable, corrupt or exhausted disk | Metadata/auth/file operations fail; `/health` may still succeed | No second authority; preserve original and investigate privately |
| Entire control host or its persistent disk lost | Frontend/API/metadata/repair/signing unavailable; old snapshots cannot authorize recovered access/signing | Restore controller needs newest baseline and cannot activate a candidate |
| One active storage machine lost | Four remaining verified shards permit retrieval; repair may use spare; new uploads select three eligible healthy configured nodes, including the spare | Local coding/restart tests; independent-host loss pending |
| Two active machines lost | Only two initial shards remain, below four required; retrieval fails for affected segments | Coding threshold cannot be repaired from metadata/registry |
| Network partition between coordinator and nodes | Node timeouts affect retrieval/repair/upload; control metadata survives | Bounded request/client timeouts; no endpoint failover guarantee |
| Arc RPC outage or ambiguous canonical history | Public registration/proof delayed/blocked; retained signed intent/ledger remains unchanged | Storage retrieval is separate from registration; no fee/nonce reset |
| Stale backup offered while live baseline survives | Restore refuses logical mismatch, including revocations and spend | Local controller/validator evidence; candidates remain gated |
| Newest baseline lost after interrupted restore | Resume cannot establish unchanged live lane; candidate remains inactive | New regression with injected process controls; no independent durability proof |

Pilot startup enables `TANK_REQUIRE_EXISTING_DATABASE=true`: SQLite `mode=rw`
rejects missing/schema-zero state instead of silently bootstrapping an empty
volume. Provision initial authority explicitly with `tank-access create`.
Local development retains the bootstrap `metadata.Open` default.

Both writable APIs now refuse `DATABASE_PATH.restore-gate` before creation or
migration, including markers reached through database symlink chains, and refuse
hard-linked/nonregular database files. New replicated checkpoints are marked
inactive before their database bytes appear. A coordinator regression verifies
rejection before listener/node setup; read-only reconciliation still works.
See [checkpoint format and limits](signing-state-recovery.md). Gates do not fence
already-open stores, authenticate the newest state, or detect arbitrary unmarked
stale copies/wrong initialized volumes. Preserve the existing process quiescence,
shared-lane controller and external-authority requirements. No guard permits
starting a standby merely because its database exists or chain nonce matches.

The four-node storage topology reduces a storage failure risk. It does not reduce
the single-control-host availability risk. DNS/load balancing to another process
with a stale SQLite copy would risk credential resurrection and nonce/budget
rollback. Sharing SQLite/WAL or a filesystem signing lock over a network mount is
not a supported distributed-coordination design. A replicated database alone
also does not fence local keystore access.

## Concrete next availability choice

The least scope for the pilot is honest single-active operation with a documented
outage window, independently durable authoritative recovery and manual promotion
with enforced old-host/custody fencing. A standby may hold verified release
artifacts and receive private encrypted backup records, but cannot serve auth,
mutate metadata or sign before complete-tip proof and promotion. The proposed
acknowledgement barrier sacrifices write availability during backup-authority
partitions. If the authority remains one independent host it is itself a write
availability dependency; specify this explicitly. Multi-host automatic failover
requires a consensus-backed authority, health/readiness design and custody fencing,
all beyond today's local locks. Do not imply a spare coordinator is now supported.

Decouple frontend hosting/proxy only after selecting origins and persistent session
secret custody. This can remove a frontend co-location outage but cannot restore
file service without coordinator metadata. A read-only standby is not automatically
safe: access revocations must be current. It must fail closed if freshness is
unknown. User-owned recovery keys cannot reconstruct missing server authorization
or manifests.

## Hosted evidence needed for a service commitment

Before stating an availability target, select failure domains, owner/operator
access, independent backup authority, session/custody recovery plan and the exact
manual promotion controls. Specify intended incident contacts, maximum outage,
content RPO, zero-loss authority requirement, and retention/funding; none is approved
here. No paid infrastructure or public deployment is performed by this review.

Measure separately on chosen hosts: coordinator kill/restart with same disk;
frontend/proxy failure; control-host power/network loss; loss of the actual primary
volume; backup-authority partition; stale/revoked credential attempts during
recovery; and old-writer restart after promotion. Record detection time, request
errors, recovery time, committed sequence, content loss, exact file byte/name
verification, isolation, repair backlog and immutable transaction/budget proof.
Recover to a fresh private path without deleting the source. Keep public reports
aggregate and secret-free. A local sleeping process or loopback database fixture
provides implementation evidence, never an independent-host availability result.

For current scoped local evidence run:

```bash
python3 -W error::ResourceWarning scripts/test-disaster-recovery-boundary.py
go test ./internal/coordinator -run '^TestRetrieveAfterNodeFailure$' -count=1
```

Independent authoritative recovery and coordinator failover remain release blockers
if the promised pilot service requires recovery from control-host loss. A pilot
that accepts planned single-host downtime still needs explicit owner agreement and
must not advertise permanent retention or automatic host failover.
