# Pilot backup and restore

Metadata and node shards are separate recovery inputs. Browser recovery keys stay
with users and are never included in a server-side backup. A metadata snapshot
contains credential hashes, authorization, manifests, filenames, and durable
jobs; store it privately and encrypt off-machine backup copies.

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

Snapshot consistency is not full-system point-in-time coordination: newly stored
files after the snapshot may be absent from restored metadata. Preserve node
shards and take metadata snapshots after successful uploads according to the
pilot's documented recovery-point objective.

## Restore drill

1. Stop only the services owned by the drill. Keep existing pilot data intact.
2. Copy a chosen snapshot into a **new** private recovery directory. Verify its
   checksum and integrity; never overwrite the live database during a drill.
3. Point the recovery coordinator and workers at that copy. Use the matching
   configuration, stable node URLs, credential secrets, and session secret.
4. Reapply credential revocations and user-access changes made after the snapshot
   before admitting real users. Old snapshots include the earlier credential
   state; restoring them can otherwise reactivate a subsequently revoked key.
5. If restoring after a crashed worker, allow durable leases to expire before
   retrying. Preserve recorded transaction hashes; never reset jobs blindly.
6. Sign in with a synthetic pilot credential, import its private browser recovery
   file, and verify exact plaintext bytes and original filename. Confirm another
   synthetic user cannot access it. Exercise reconstruction and automatic repair
   using the separate-machine acceptance procedure.
7. Record snapshot age, data recovered, measured restore time, and missing writes.

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
