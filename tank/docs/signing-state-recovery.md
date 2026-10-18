# Replicated signing-state checkpoints

This adds private, verified copies of one consistent metadata snapshot. It does
not activate recovery, establish the newest authoritative state, or provide
coordinator/signer failover. The existing restore gates remain unchanged. Read
[the disaster recovery authority design](disaster-recovery-design.md) and
[the existing restore procedure](hosted-restore-safety.md) before recovery.

## Create and verify copies

Build `go build -o bin/tank-backup ./cmd/tank-backup`. Provision private parent
folders on operator-controlled backup mounts, then choose new bundle names:

```bash
./bin/tank-backup --db /private/live/tank.sqlite \
  --replica /private/backup-a/checkpoint-UNIQUE_TIMESTAMP \
  --replica /private/backup-b/checkpoint-UNIQUE_TIMESTAMP \
  --timeout 2m
./bin/tank-backup --verify /private/backup-a/checkpoint-UNIQUE_TIMESTAMP
```

Repeat `--replica` at least twice; it cannot be combined with the existing
`--out` raw-snapshot option. Each bundle contains `snapshot.sqlite` and a final
`manifest.json` with SHA-256, size, format version, and creation time. Exactly one
WAL-consistent snapshot is created; all replicas contain identical bytes. Files
are mode 0600 and directories mode 0700. Publication refuses existing paths and
symlink aliases, flushes files/directories, and verifies each completed copy.
Keep parent directories under exclusive operator control during publication.
No transfer, backup service, scheduler, retention deletion, or paid storage is
configured by these commands.

The operator must attest that the mounts survive loss of the original host and
have separate custody/failure domains. Two directories, two containers, or two
paths to the same storage do not prove independence. Network/filesystem durability
and encryption at rest depend on the selected mount/backend; validate them with
actual host-loss drills. Private snapshots include credential hashes, permissions,
filenames, manifests, journals, and spend history. They exclude user recovery keys,
node shards, signing keystores, passwords, and external deployment journals, which
need separate appropriate custody. No secrets should be printed or committed.

Failure preserves published and partial directories. An error reports the number
of fully verified copies; do not assume the requested set completed. A missing,
invalid or mismatched manifest fails verification. Preserve every surviving copy
and inspect failures privately. Do not reuse or overwrite failed destination names.
The context bounds snapshot/copy/hash work; underlying filesystem calls, including
fsync on an unresponsive mount, remain subject to the filesystem's own timeouts.

For an expected generation whose digest was separately recorded in trusted custody:

```bash
./bin/tank-backup --verify /private/backup-a/checkpoint-UNIQUE_TIMESTAMP \
  --expected-sha256 EXPECTED_DIGEST
```

Verification checks bytes only. The manifest is not signed; an attacker who can
rewrite both files can replace it. A separately pinned digest identifies one
expected generation, but cannot prove no later mutation exists. A timestamp or
matching chain nonce does not establish that either. Keep bundles immutable in
operator custody; restore only to a new inactive private path, never operate a
worker directly on the backup copy.

## Lost newest database: signing stays blocked

These checkpoints preserve signed bytes, permanent reservations and revocations
that committed before their snapshot. Later acknowledged changes can still be
missing. Loss of the newest live comparison baseline continues to block the
existing restore controller, even if a checkpoint passes byte verification and
chain nonce appears unchanged. Never remove gates or resume signing from such a
copy. Raw database APIs may open a stale snapshot; that is not recovery approval.

Safe activation requires the complete newest authoritative records: principals,
credential hashes/expiry/revocation tombstones and access changes; all manifests,
placement/ownership/filenames and durable jobs; account/chain/registry identity,
nonce intent and original signed bytes/hash; every permanent maximum-cost
reservation, approved budget and cumulative total; external deployment journals;
and persistent maintenance/controller state. An authenticated independent current
tip and proof the old writer/custody is fenced are also required. Missing or
ambiguous records leave recovery blocked. User-held encryption recovery keys and
enough verified shards are separately required to recover plaintext.

Closing the newest-loss gap requires the synchronous authority protocol described
in [the existing design](disaster-recovery-design.md): every mutation must be
serialized and durably acknowledged by an independent authority before success;
nonce ownership/budget intent must persist before signing; custody must retain
original bytes independently before releasing them; recovery replays the complete
sequence to an authenticated current tip. Epoch enforcement must reject old writers
at both storage and custody. A post-sign backup wrapper, timer, copied lock file,
or expired lease does not close the signing uncertainty or split-brain window.
Those services and activation protocol remain unimplemented.

## Local evidence

`go test ./internal/metadata ./cmd/tank-backup` covers synthetic primary loss with
revocation denial, signed-byte preservation, exact permanent budgets/reservations,
user isolation, identical replicas, corruption/incomplete bundle rejection,
expected-generation mismatch, cancellation, aliases and overwrite refusal.
`python3 scripts/test-disaster-recovery-boundary.py` covers lost-newest-state
rejection by the existing restore controller. These are local regression checks;
they do not establish independent-host durability, authoritative newest-state
recovery, or automatic failover.
