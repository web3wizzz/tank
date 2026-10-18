# Authority mutation coverage and migration boundary

Source inventory at `da61cab`, schema 8. This is an implementation plan for the
[journal contract](authority-journal-protocol.md), not completed write replication
or permission to recover. All current production mutations still commit locally
in SQLite. No authority adapter is wired, no database is migrated by this document,
and loss of newest authoritative state remains signing-blocked.

## Current durable state

The schema contains eleven application tables. A bootstrap baseline must include
all eleven, their constraints/indexes, schema version and the approved release and
operational policy binding. Do not build genesis from `RestoreSnapshot`, which
captures only registration jobs/lanes/spend, or from a list of registered file IDs.

| Tables | Required state |
| --- | --- |
| `principals`, `api_credentials` | Principal IDs/labels; credential IDs, principal binding, token hashes, expiration and revocation flags |
| `file_access`, `file_names` | Per-principal grants/names and legacy file names, preserving first-name rules |
| `manifests` | Full stored manifest JSON, coding/commitments, creation time, size and current verified node locations |
| `repair_jobs`, `registration_jobs` | All jobs, status, attempts, due times, lease tokens/deadlines, recorded errors and registration hashes |
| `upload_leases` | Global admission ownership/token/deadline; an expired lease is still part of the baseline |
| `registration_transactions` | Current account lane: chain/account, job/target, nonce, exact signed bytes/hash and prepared/confirmed state |
| `registration_spend_accounts`, `registration_spend_reservations` | Exact approved budget and cumulative reserved decimal integers; permanent per-hash maximum costs |

SQLite snapshots/checkpoint digests cover the complete current database bytes.
`pilot-restore.py:logical_digest` additionally compares all non-internal schema
objects and all table rows with a frozen surviving newest baseline. Neither digest
authenticates a fresh tip after that baseline is lost. Deployment evidence,
approved fee policy, custody configuration, runtime credentials and session/TLS
secrets also need separately reviewed private custody; do not copy secrets into
the metadata journal merely to make it appear complete. Browser recovery keys
remain exclusively user-owned.

## Every current application mutation

The following 22 public store methods write authoritative application state.
Links identify the current implementation; methods grouped in a row retain their
individual transaction boundaries. A future writer must cover every caller,
including administrators and maintenance, rather than only HTTP upload handlers.

| Store methods and source | Current write/precondition boundary | Replay material that must be frozen |
| --- | --- | --- |
| [`CreateAccessKey`](../internal/metadata/access.go) | One transaction inserts principal and credential | Generated IDs, label, token **hash**, expiration; never the raw token |
| `IssueAccessKey`, `RevokeAccessKey` (same source) | Separate insert for an existing principal; revocation update requires a matching credential | Principal/key binding and immutable credential fields; revocation tombstone and original outcome |
| `GrantFileAccess` (same source) | Manifest and principal must exist; upsert preserves the first nonempty per-user name | Principal/file IDs, normalized name and previous grant condition |
| [`Save`](../internal/metadata/sqlite.go) | Insert-only manifest; existing file yields `ErrExists` | Exact manifest bytes and file-ID binding; do not invent a new timestamp on replay |
| [`SaveFilename`](../internal/metadata/filenames.go) | Manifest must exist; first filename wins | Normalized legacy filename and original insert/no-op result |
| [`ReplaceLocations`](../internal/metadata/locations.go) | Compare-and-swap exact old/new JSON, preserving identity/commitments | Both manifest versions and the verified repair outcome; never recompute placement during replay |
| [`EnqueueRepair`](../internal/metadata/jobs.go) | Manifest must exist; one job per file; enqueue is idempotent | File ID and original next-attempt time |
| `ClaimRepair` (same source) | Atomic `UPDATE … RETURNING` selects the first due/unleased job and increments attempts | Selected job, generated token, evaluation time, new lease/attempt values; do not reselect with today's clock |
| `CompleteRepair`, `RetryRepair` (same source) | Delete or update only for the unexpired matching lease | Job/token, evaluation time, due time and bounded stored reason; original success/failure |
| [`AcquireUploadLease`, `ReleaseUploadLease`](../internal/metadata/quotas.go) | Acquire upserts only an expired global lease; release deletes only matching ownership | Generated token, commit-time clock/preconditions, stored deadline (seconds, unlike job milliseconds) |
| [`EnqueueRegistration`](../internal/metadata/registrations.go) | Manifest/target validation; one job per file/target | Exact target and original next-attempt time |
| `ClaimRegistration` (same source) | Atomic `UPDATE … RETURNING` selects a due pending/submitted job, claims lease and increments attempts | Selected job/target/hash, token, evaluation time and new lease/attempt values |
| `SaveRegistrationTransaction` (same source) | Active lease; immutable-or-empty hash; marks submitted | Exact hash, prior job state and evaluation time; used by the separate local registration worker |
| `CompleteRegistration`, `RetryRegistration`, `FailRegistration` (same source) | Matching active lease; terminal/update transitions clear lease | Original job/token/status, evaluation time, next attempt/reason and outcome |
| [`SavePreparedRegistration`](../internal/metadata/transactions.go) | One transaction updates job hash and account lane; same prepared bytes are idempotent; successor requires confirmed nonce + 1 | Exact signed bytes/hash/nonce/account/target/job binding and original lease preconditions |
| `CompletePreparedRegistration` (same source) | One transaction marks job registered and lane confirmed after canonical checks in registry layer | Exact lane/job/hash, original lease checks and separately bound canonical observation evidence |
| [`ReserveRegistrationSpend`](../internal/metadata/registration_spend.go) | One transaction validates signed dynamic-fee bytes/lease/lane, pins budget, inserts permanent per-hash cost and increments total | Exact unsigned terms/signed binding, approved budget, prior totals and worst-case cost; retries never double-charge, confirmations never refund |

`migrate` in [`migrations.go`](../internal/metadata/migrations.go) is an additional
schema authority boundary: it obtains `BEGIN IMMEDIATE` before reading/upgrading
the version. Writable opens also set local SQLite pragmas. A future authority mode
must prevent direct administrative/bootstrap/migration bypass, while retaining
explicit first-time bootstrap as a separately reviewed operation. Backups use
`VACUUM INTO` to write a new destination, not to append a source mutation. The
restore controller uses `mode=rw` plus `BEGIN IMMEDIATE` to freeze writers; it does
not promote candidates. Tests/fixture seed SQL is not an authorized production
mutation path.

Production Go SQL is currently encapsulated in `internal/metadata`. Searching only
`ExecContext` misses the two claim methods because their writes use query APIs.
Future review must inspect SQL semantics as well as call sites. Direct operator
SQL, old binaries and unmodified administrator tools must be excluded/fenced in
any future replicated mode; a new wrapper around coordinator alone is insufficient.

## Deterministic command and acknowledgement design

Generate IDs/tokens/timestamps once before append, and bind the selected operation
and preconditions to its immutable request. Persist authoritative token hashes;
never persist raw bearer tokens or browser recovery keys. Replay must not call the
random generator, use a current clock to re-evaluate historical leases, re-query
an unpinned chain, choose different nodes or invoke custody. The independent writer
must validate the requested transition against the preceding committed state; a
caller-provided resulting digest alone is not validation.

Preserve existing multi-row atomicity: principal+credential creation,
job+prepared lane, job+confirmed lane, and permanent reservation+account total.
Current upload handling acquires a lease, writes the manifest, then grants access
or saves a legacy name in separate commits before returning success. A failed or
lost HTTP response does not imply that none of these commits exists. Migration
must either preserve each ordered transition and its resolved result, or introduce
a separately reviewed atomic command; it cannot infer rollback from HTTP status.

An append accepted remotely but unapplied locally must be replayed before reads,
acknowledgements or subsequent mutations resume. Resolve an uncertain append by
the original operation ID even after process loss. Unknown outcome must not be
converted into a new credential, nonce, reservation or placement. If a credential
was committed but its one-time raw token response was lost, resolve the original
operation first; an authorized revocation/new issuance is a separate operation.
Do not archive the raw token to make retries return it.

## Signing gaps that ordinary write interception cannot close

Current [`PrepareTestnetRegistration`](../internal/registry/prepared.go) calls
`SignTransaction`, validates the returned signature/bytes, then calls
`SavePreparedRegistration`. The broadcast worker later reserves maximum spend
before `Submit` when it has no canonical successful observation. Existing gates
ensure no new signature for a surviving prepared lane and no broadcast without
the required reservation. This order is **not** the proposed independent durable
intent-before-sign or custody-byte-persistence-before-return protocol.

A crash or ambiguous custody response between signature creation and its first
durable journal write remains unresolved. Adding replication only inside
`SavePreparedRegistration`, or taking a checkpoint after it, cannot close that
window. Required future commands reserve exact unsigned account/nonce/terms and
worst-case budget before custody, then custody durably retains original immutable
bytes independently before releasing them. If those bytes/intent cannot be
recovered, keep signing blocked regardless of latest/pending chain nonce equality.

The current schema keeps **one latest lane per chain/account**. Preparing the next
nonce after confirmation replaces that row's previous raw bytes. Older
registration jobs retain hashes and the spend ledger retains permanent costs,
but those tables do not contain the full older signed transactions. A snapshot
therefore captures all bytes still stored, not an immutable lifetime archive.
The current read-only reconciliation checker fetches confirmed transaction bytes
for each retained job hash and checks canonical observations, contiguous nonces
and exact signed-cost reservations against the surviving baseline. That is useful
confirmation evidence, not proof that a lost baseline omitted no later intent,
revocation or reservation.
Migration must inventory and preserve every surviving original journal/backup and
establish complete historical transaction evidence; never fabricate missing bytes
from hashes or claim an old snapshot is authoritative merely because it validates.
An append-only authority must retain all historical intent/byte records before
the materialized account lane advances. Missing required history is a migration
and recovery gate, not a reason to reset the account or fee budget.

Finally, storage epoch rejection cannot fence an already running local keystore
or prevent broadcasting already-created bytes. The independent epoch authority,
enforced custody ownership, old-host isolation and retained nonce/budget history
must work together before any host-loss promotion. No such promotion is enabled.

## Next implementation gates

1. Specify versioned semantic commands, trusted evaluation time and complete-state
   digest/bootstrap. Exercise the transitions above against synthetic state before
   changing production store methods. Reject unsupported schemas/commands.
2. Implement a separate materialized-state applied-sequence marker in the same
   transaction as replay. Fault-test remote append, local apply and response
   boundaries; reject stale reads and any unreplicated writer. Design migration
   and rollback before selecting a new schema version; do not open private data
   to try a migration during development.
3. Implement and fault-test custody intent/bytes persistence and writer fencing;
   preserve existing budgets and lane history. Keep all runtime routing disabled
   until complete coverage, independent persistence and safe bootstrap are proven.
4. Select/provision independently operated backend custody only with owner inputs
   and authorization; validate fresh-tip transport, off-machine durability and
   old-writer fencing on actual hosts. Local fixtures cannot satisfy those gates.
