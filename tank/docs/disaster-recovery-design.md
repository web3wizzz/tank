# Disaster recovery when the newest control database is lost

Status: reviewed design proposal and isolated regression evidence; no independent
backup service, authority journal, remote signer or restore activation is
implemented by this document. Current signing remains one host/one account.
Loss of the newest live comparison baseline blocks the existing restore controller.

## The gap and required recovery objective

`internal/metadata/backup.go` publishes a private, integrity-checked, consistent
SQLite snapshot including committed WAL state. SQLite FULL synchronization protects
local commits; neither mechanism creates an independent durable authority. A
snapshot taken before a revocation, access change, new signed transaction or spend
reservation can be internally valid and still unsafe. `backup_test.go` explicitly
shows that a credential revoked after a snapshot still authenticates in that
snapshot. `scripts/pilot-restore.py` compares every table with a frozen newest live
baseline. If that baseline is lost, an older snapshot cannot satisfy the proof.

Canonical chain reconciliation is necessary but insufficient: it cannot recover
credential revocations, access grants, filenames, manifests, unbroadcast signed
bytes, or a transaction known only to a disconnected mempool. Equal latest/pending
nonces do not prove that no signed bytes escaped. Confirmed actual fees cannot
replace permanent maximum-cost reservations. A recent backup timestamp, checksum,
successful download or node redundancy does not establish an authoritative tip.

Recovery objectives require owner decisions. Proposed safety objective: zero loss
of acknowledged authorization changes, nonce ownership, signed transaction bytes
and permanent budget reservations after loss of the control host. Ordinary file
content RPO and recovery time can be separately measured. No zero-loss or recovery
time claim is presently supported. If the independent authority is unavailable,
mutations and signing stop; availability must not weaken this guarantee.

The public `tank-prepare-registration` entry point now refuses a missing journal
and opens only an existing initialized database before custody or RPC setup.
Its maintenance/concurrent-worker guard remains enforced. Tests reject missing
and schema-zero authority without creating a database or reaching custody. This
closes silent bootstrap after loss, not rollback to an initialized stale copy.
There is still no independent authenticated tip capable of approving that copy.

## Proposed independent authority

Use an operator-selected service on a distinct host/storage failure domain with
private authenticated transport, encryption at rest, independently held recovery
credentials, durable append-only retention and tested restore access. Its actual
backend, host identity, retention and cost need owner review; none is provisioned.
A second directory, container, disk attached only to the same host, or asynchronous
copy does not meet control-host-loss durability. To survive loss of both the source
and one backup domain, a further independent durable copy/quorum is required and
must be stated as a different failure objective.

The service stores a versioned ordered authority stream per pilot, anchored by an
explicit genesis state/schema/release digest. Each accepted command has a unique
idempotency ID, epoch, previous digest, increasing sequence, full validated mutation
payload, and resulting digest. Its append acknowledgement binds those fields and
is returned only after durable persistence. A locally remembered high-water mark
is insufficient: recovery queries the independently authenticated current tip.
Retain tombstones and all historical signed bytes rather than only the latest lane.
Private records are never published in Git, public reports or general logs.

The stream covers **every** authoritative mutation, including:

| State | Required durable contents |
| --- | --- |
| Identity and authorization | Principal/credential hashes, revocations, expirations, access grants/removals, and policy changes |
| File metadata | Complete manifests, placement updates, encrypted filename metadata, ownership, quotas and durable repair/upload outcomes |
| Registration | Job identity/status, manifest binding, chain/account/registry, deployment evidence, nonce intent, signed bytes/hash and observation history |
| Budget | Approved policy, permanent maximum-cost reservation per intent/transaction, cumulative totals; no refunds from receipt cost |
| Authority | Epoch ownership, unresolved operations, committed tip and release/schema binding |

Browser recovery keys remain user-owned and excluded. Runtime tokens, TLS keys and
custody recovery inputs need a separately reviewed encrypted recovery plan; a
metadata backup is not a keystore backup. Neither plan transfers secrets now.

## Commit, signing and fencing boundaries

This requires a real mutation protocol, not a timer around `tank-backup`.
Initially retain one active coordinator and one signer. Route every coordinator,
maintenance, administrator and registration mutation through the same authority
writer; there must be no direct SQLite write bypass. The independent stream is the
commit authority, and SQLite is its materialized local state. Serialize commands,
validate them against the preceding committed state, append durably with an epoch
and compare-and-append tip, then atomically apply that committed sequence locally.
Reads/acknowledgements are served only from a fully applied committed prefix.
An uncertain append result is resolved by the same operation ID at the authority;
it cannot be replaced with a new operation. A crash after remote append and before
local apply is recovered by replay; no success response or downstream operation
may use a locally unapplied tip. Snapshot publication is tied to a committed
sequence and state digest, followed by immutable incremental records through the
authenticated tip. A chain digest alone cannot prevent truncation by the writer;
only the independent current-tip/epoch authority supplies the rollback boundary.

Before reaching signing custody, durably commit an intent owning the exact account,
nonce, transaction terms and worst-case budget charge. An unresolved intent keeps
that nonce and charge occupied even if the local machine disappears. The signing
boundary must durably retain the resulting immutable signed bytes independently
**before returning them** to a caller that can broadcast. A signer-side crash after
signing but before persistence remains unresolved and blocks new signatures until
the original bytes are recovered or a separately reviewed resolution proves safe.
Do not re-sign merely because a nonce query is unchanged. A wrapper that backs up
after the current local signer returns bytes leaves this uncertainty window and
must not claim to close it.

The independent authority grants monotonically increasing writer epochs and
rejects stale appends. Custody enforces the current epoch before each signing
operation and does not release bytes without durable recording. Recovery also
requires the old host/custody access be fenced; a copied filesystem lock or an epoch
field ignored by a local keystore cannot stop an old process signing/broadcasting.
Already-created bytes may still be broadcast by an old host, so their original
nonce ownership and maximum reservation must remain represented. Lease expiry
alone cannot prove an old signer stopped. Until enforceable custody fencing and
all mutation paths exist, host-loss recovery remains signing-blocked.

## Recovery procedure after implementation

1. Isolate/fence the failed control host and its custody access using independently
   verified ownership controls. Keep public registration disabled and preserve
   every surviving database, snapshot, signed journal and shard copy.
2. Authenticate the independent authority, obtain its tip and unresolved operation
   inventory, and acquire an epoch only after the old writer is fenced. If tip,
   custody, fencing or any record is unavailable/ambiguous, remain blocked.
3. At a new private inactive path, verify the snapshot schema/state digest, replay
   the contiguous journal to that tip, and reject gaps, duplicate conflicts,
   truncation, invalid digests, unsupported releases and epoch regressions. Apply
   revocation tombstones and access removals exactly; never merge by timestamps.
4. Recompute cumulative permanent reservations and nonce ownership, verify every
   immutable signed byte/hash and manifest binding, and perform existing canonical
   receipt/registry/nonce checks. Pending or uncertain signed intents remain blocked
   under the current validator policy; do not broadcast to make proof pass.
5. Verify shards, synthetic encrypted retrieval/name recovery and cross-user denial;
   measure the recovered committed sequence, content RPO and elapsed time privately.
6. Activate through a separately implemented/reviewed service-switch controller
   only after all proofs. The current controller cannot activate a candidate and
   must not be repurposed or have gates deleted for this procedure.

## Implementation and acceptance gates

A focused next implementation is an authority interface and synthetic fault-injection
backend, followed by coverage of all metadata mutation paths and signer-side durable
intent/bytes protocol. A production backend and custody epoch enforcement then need
selected independent infrastructure and operator access. Schema compatibility,
initial authoritative bootstrap from existing private journals, and import of all
historic reservations/bytes require a dedicated migration review. Do not reset the
existing lane or change the approved 65 Gwei / 0.008 registration / 0.10 aggregate /
0.04 deployment testnet USDC ceilings.

Required executable evidence before claiming this gap closed:

| Failure injected | Required result |
| --- | --- |
| Source destroyed after acknowledged revocation | Recovered credential stays denied; other-user isolation holds |
| Source lost before/after remote append, local apply and response | Deterministic replay/idempotency; no success at an uncommitted tip |
| Source lost before/after intent reservation, signing, byte persistence and send | No nonce reuse, no replacement signature, exact original bytes, cumulative cap retained |
| Independent backend partition, unknown acknowledgement or disk/fsync failure | No mutation acknowledgement/signature/broadcast; occupied intents preserved |
| Old writer restarts or partitions during promotion | Stale epochs rejected at storage and custody; only one active authority |
| Older valid snapshot/log prefix substituted or segment removed | Independent tip detects rollback/gap; gates remain closed |
| Independent restore to fresh control machine | Exact state/bytes/revocations verified, authenticated tip recovered, measured RPO/RTO; original remains fenced |

`scripts/test-disaster-recovery-boundary.py` adds current-controller regression
checks with disposable synthetic databases and injected process-control outcomes: newest live
state loss before execution and after an interrupted copy both prevent progress,
even with an injected successful validator. These prove the fail-closed boundary;
they do not implement independent backup or prove recoverable signing/hosted HA.
