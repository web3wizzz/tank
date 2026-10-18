# Pilot backup and restore

For the implemented signing fence, strict stale-backup rejection, canonical
read-only reconciliation and interrupted local controller recovery, see
[failure and restore safety](hosted-restore-safety.md). Remote activation remains
disabled; an old snapshot is never treated as authoritative signing/access state.

Metadata and node shards are separate recovery inputs. Browser recovery keys stay
with users and are never included in a server-side backup. A metadata snapshot
contains credential hashes, authorization, manifests, filenames, durable
jobs, and any prepared signed-transaction journal; store it privately and encrypt off-machine backup copies.

## Online metadata snapshots

Build the local command:

```bash
go build -o bin/tank-backup ./cmd/tank-backup
mkdir -m 700 -p data/backups
./bin/tank-backup --db data/local-demo/tank.sqlite --out data/backups/metadata-UNIQUE_TIMESTAMP.sqlite
```

Choose a new output name for every snapshot. The command refuses an existing
file or symlink, requires an existing regular source database, and does not
migrate that source. It uses SQLite `VACUUM INTO` to include committed WAL state,
checks integrity and foreign keys, and publishes a mode-0600 snapshot atomically.
Concurrent writes continue to work; a snapshot represents a consistent point in
time. The default deadline is two minutes; `--timeout` accepts 1s–1h.
Do not copy only the live `.sqlite` file while its WAL is active.

For replicated copies, use [checkpoint bundles](signing-state-recovery.md) with
at least two explicit operator-provisioned destinations. One consistent snapshot
is copied and verified identically; new version-2 bundles carry an inactive marker
before database bytes are published. Writable service startup refuses marked
copies. The raw `--out` format above remains compatible and unmarked; it is not an
activation permit. Neither format establishes the newest acknowledged state.
Operators must independently validate off-machine durability, custody, encryption
and restore access. Two directories or a matching chain nonce cannot establish
safe recovery after the newest authoritative database is lost.

Snapshot consistency is not full-system point-in-time coordination: newly stored
files after the snapshot may be absent from restored metadata. Preserve node
shards and take metadata snapshots after successful uploads according to the
pilot's documented recovery-point objective.

## Restore drill

1. Stop only the services owned by the drill. Keep existing pilot data intact.
2. Compare the chosen snapshot's complete schema/table state with the frozen,
   newest live database. Reject any mismatch, including jobs, nonce/spend history,
   credentials and access changes. Publish only at a **new** private recovery path;
   verify integrity and never overwrite the live database during a drill.
3. **Pause the public registration worker before any restore.** Keep the newest
   signed-transaction journal and spend reservations from the live database. A
   snapshot can predate a signed or submitted transaction; restoring it as the
   active worker database can lose nonce ownership and understate already-spent
   budget. Never resume broadcasting from a stale snapshot alone.
4. Keep the candidate inactive and signing-blocked. Run the read-only validator
   against every historical signed transaction, canonical receipt/registry record,
   account nonce and cumulative reservation. Preserve all bytes/hashes. The local
   controller permits only the unchanged original lane to resume after proof;
   remote candidate activation is not implemented or enabled.
5. Reject snapshots missing any newer credential revocation or user-access change.
   Never reactivate an earlier credential state or guess a merge to make a drill
   pass. Recovery without the newest live comparison baseline needs separately
   reviewed authoritative history and remains blocked.
6. If restoring after a crashed worker, allow durable leases to expire before
   retrying. Preserve recorded transaction hashes; never reset jobs blindly.
7. Sign in with a synthetic pilot credential, import its private browser recovery
   file, and verify exact plaintext bytes and original filename. Confirm another
   synthetic user cannot access it. Exercise reconstruction and automatic repair
   using the separate-machine acceptance procedure.
8. Record snapshot age, data recovered, measured restore time, and missing writes.

## Node storage and custody

Use a persistent local filesystem per node, one node process per directory, and
stable endpoints. Shard writes are atomic, but copying a live directory can race
with repairs. Quiesce the particular node or use an atomic filesystem/volume
snapshot, and preserve the complete directory without deleting retained files.
A metadata snapshot cannot reconstruct shards lost beyond the coding threshold.
Three active machines plus one spare permit one-active-machine loss and repair.

Keep off-machine copies in storage controlled by the operator. Retention, restore
testing, encryption, and funding belong in the pilot agreement. No automatic
backup deletion, paid backup service, or remote backup transfer is enabled here.
A schedule is useful only after a measured restore drill succeeds.
