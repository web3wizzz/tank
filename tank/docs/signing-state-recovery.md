# Replicated signing-state checkpoints

This adds private, verified copies of one consistent metadata snapshot. It does
not activate recovery, establish the newest authoritative state, or provide
coordinator/signer failover. New bundles are inactive, and writable metadata opens
honor database restore markers. Existing controller activation remains disabled. Read
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
`--out` raw-snapshot option. Each new version-2 bundle contains `snapshot.sqlite`,
a private `snapshot.sqlite.restore-gate`, and a final `manifest.json` with SHA-256,
size, format version, and creation time. The marker is durably published before
the snapshot path appears. Writable metadata `Open`/`OpenExisting` calls reject
marked paths, including database symlink aliases, before creation or migration.
Existing nonregular database files and files with multiple hard links are also
refused before writable opening: their aliases cannot safely coordinate SQLite
journals/locks or adjacent markers. Refusal never removes a link or marker.
A present, malformed, symlink, or inaccessible marker blocks writable opening;
never remove a marker to activate a copy. Exactly one WAL-consistent snapshot is
created; all replicas contain identical bytes. Files
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
invalid or mismatched manifest fails verification. Version-2 verification also
requires the original regular, private, operator-owned inactive marker; a missing,
damaged or symlink marker fails verification. Preserve every surviving copy
and inspect failures privately. Do not reuse or overwrite failed destination names.
The context bounds snapshot/copy/hash work; underlying filesystem calls, including
fsync on an unresponsive mount, remain subject to the filesystem's own timeouts.

For an expected generation whose digest was separately recorded in trusted custody:

```bash
./bin/tank-backup --verify /private/backup-a/checkpoint-UNIQUE_TIMESTAMP \
  --expected-sha256 EXPECTED_DIGEST
```

Verification checks snapshot bytes and, for version 2, its inactive marker.
Legacy version-1 bundles remain supported for byte-integrity inspection: they have
no required marker and verification grants no activation permission. Preserve them
as inactive recovery inputs; this compatibility does not retrofit gating onto
old bundles or make an unmarked copy safe. The manifest is not signed; an attacker
who can rewrite both files can replace it. A separately pinned digest identifies one
expected generation, but cannot prove no later mutation exists. A timestamp or
matching chain nonce does not establish that either. Keep bundles immutable in
operator custody; restore only to a new inactive private path, never operate a
worker directly on the backup copy.

## Lost newest database: signing stays blocked

These checkpoints preserve signed bytes, permanent reservations and revocations
still present when the snapshot is taken. The current account lane holds only its
latest transaction's raw bytes; older jobs and spend records retain hashes/costs,
not a lifetime signed-byte archive. Preserve all surviving original journals and
backups. See the [mutation/history inventory](authority-mutation-map.md).
Later acknowledged changes can still be
missing. Loss of the newest live comparison baseline continues to block the
existing restore controller, even if a checkpoint passes byte verification and
chain nonce appears unchanged. Never remove gates or resume signing from such a
copy. Read-only `OpenRestoreReadOnly` and `BackupDatabase` can inspect marked
snapshots without enabling writes. Copies made through the raw `--out` workflow
are not automatically marked; neither that API nor an arbitrary unmarked copied
database can prove freshness. The presence check does not fence an already-open
store or replace controller coordination. It does not protect against an operator
removing a marker or making a separate malicious copy that omits it; neither is an
authorized recovery procedure.

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

If an existing installation is refused for an unsafe alias/link layout, preserve
all database copies, links and gates for operator review. Never unlink a gate or
change a journal path to force startup. Verification of a version-2 marker must
run as its owning service/operator identity; differing host UID/mount mappings
need an explicit custody/permissions plan, not automatic ownership changes.

## Local evidence

`go test ./internal/metadata ./cmd/tank-backup` covers synthetic primary loss with
revocation denial, signed-byte preservation, exact permanent budgets/reservations,
user isolation, identical replicas, corruption/incomplete bundle rejection,
expected-generation mismatch, cancellation, aliases and overwrite refusal. It
also covers version-2 marker rejection, writable bootstrap/open blocking through
symlink aliases, hard-link/nonregular-file refusal without database modification,
read-only inspection, and legacy
version-1 byte-integrity compatibility.

Compiled contract artifacts used by reconciliation/authorized broadcast must be
regular files, at most 4 MiB. Direct or symlinked named pipes are rejected without
waiting for a writer; ordinary compiled-file symlinks remain supported. Broadcast
validates the artifact and budget configuration before custody/RPC setup; read-only
reconciliation validates its artifact before RPC. This does not bound stalled
regular-file I/O on an unresponsive filesystem or change activation permissions.

`python3 scripts/test-disaster-recovery-boundary.py` covers lost-newest-state
rejection by the existing restore controller. These are local regression checks;
they do not establish independent-host durability, authoritative newest-state
recovery, or automatic failover.
The loopback Arc/TLS restore integration additionally verifies both replicas
against canonical history and the permanent spend ledger, refuses their writable
activation, and resumes only the original lane with unchanged bytes and exactly
one signature/submission. This is a local generated-custody fixture.
