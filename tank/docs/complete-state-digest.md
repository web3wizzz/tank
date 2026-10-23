# Experimental complete metadata state digest

`metadata.DigestCompleteState` is a read-only, versioned logical fingerprint for
the future [authority journal](authority-journal-protocol.md). It has no production
caller, command, restore activation or signer integration. The existing restore
controller's Python digest and all existing journals keep their current format.
**A digest does not prove freshness, independent durability or permission to sign.**
Loss of the newest authoritative database/journal remains fail-closed, including
when an older snapshot has a matching chain nonce.

## Supported input and consistency

The function requires an existing regular schema-8 database with application ID
zero, UTF-8 database encoding, and the exact reviewed migration DDL. It rejects
final-component database symlinks, unknown tables/indexes/triggers, missing schema
objects, integrity failures and foreign-key violations. Equivalent but differently
formatted DDL is unsupported; changing the schema requires explicit format review.
TEXT cells must contain valid UTF-8. BLOBs retain arbitrary bytes.

One SQL read transaction covers schema/version checks and all rows, including
committed WAL contents. The complete eleven-table inventory is fixed and includes
credentials/revocations, grants, names, manifests, repair/registration jobs, upload
leases, the account transaction lane and permanent spend accounts/reservations.
Expired leases, old job errors and exact decimal TEXT remain part of the state.
See [mutation coverage](authority-mutation-map.md) for the authoritative fields.

The source is opened through the existing SQL read-only schema-8 path: no creation,
migration, writable activation or restore-gate removal. Inactive checkpoint
candidates can be fingerprinted while remaining blocked. SQL read-only mode is
not a guarantee that SQLite never needs shared-memory/WAL sidecars on a live DB.

## Binary format version 1

SHA-256 and the framing below are normative. `U(n)` is an unsigned 64-bit
big-endian integer; `F(b)` is `U(byte length) || b`. Names, declared types and TEXT
are UTF-8 bytes with no Unicode, JSON, decimal or whitespace normalization.
Domains below end with exactly one NUL byte. Counts and lengths use `U`.

1. Schema digest: SHA-256 of `tank-complete-schema-v1\0 || U(8) || U(object count)`
   followed by `F(type) || F(name) || F(tbl_name) || F(sql)` for each non-internal
   `sqlite_schema` object. Exclude names matching `sqlite_*`; sort by type then name
   with SQLite BINARY collation. Auto-index details derive from the pinned DDL.
2. Row digest: SHA-256 of `tank-complete-row-v1\0 || U(column count)` followed by
   each typed cell in `PRAGMA table_info` order. NULL is tag `00` alone; INTEGER is
   tag `01` plus eight-byte signed two's-complement big-endian; TEXT is tag `02`
   plus `F(text bytes)`; BLOB is tag `03` plus `F(blob bytes)`. REAL and values
   whose storage class disagrees with the declared column type are rejected.
   NULL is allowed only where the pinned schema permits it, including SQLite's
   nullable non-INTEGER primary keys.
3. State digest: SHA-256 of `tank-complete-state-v1\0 || schema digest || U(11)`.
   For each application table in ascending name order, append `F(table name)`,
   `U(column count)`, and each `F(column name) || F(declared type)`, then
   `U(row count)` and lexicographically sorted 32-byte row digests. Duplicates
   remain present. Physical layout, rowids, insertion order, WAL offsets and SQL
   value-collation ties cannot change this logical multiset representation.

Pinned synthetic vectors (never publish fingerprints of actual private state):

```text
schema-8 DDL ca07c04ced68800e1106d56cd47a7ee2006d2672d387372e7b1350197ae63300
empty state 250b23977bf0b80213e087fcdbef568458303a528dfcda4d11877b714ab262bd
```

These are a separate format from the journal's existing JSON-based genesis and
record hashes. Future integration must explicitly bind the state-digest format,
schema, approved release and policy in reviewed bootstrap/migration handling.

## Bounds and refusal behavior

Hard limits are 100,000 rows **per table**, 1 MiB per TEXT/BLOB cell and 64 MiB of
cell payload across all tables. INTEGER contributes eight bytes and NULL zero;
format overhead is separately bounded by schema, row and column limits. Schema
inspection accepts at most 256 objects, 16-byte object types, 128-byte names and
16 KiB DDL; the pinned tables have at most 16 columns each. SQL projections check
cell and schema-field byte lengths before returning values to Go. Oversized
values are rejected rather than truncated and accepted. Fixed-size row digests
are sorted one bounded table at a time. SQLite's internal/temp allocations remain
backend-dependent, so this is not a process memory sandbox for hostile files.

The operation caps its context at two minutes; filesystem/backend I/O may still
be uninterruptible. Failure returns a zero result with a fixed sanitized error
(or the context cancellation/deadline error), never paths, rows or fingerprints.
Treat successful results as private. There is no logging or raw-state export.

## Evidence and remaining recovery work

Synthetic Go race tests cover every table, NULL/Unicode/NUL/binary cells, exact
decimal values, revoked credentials, malformed schema/types/references, bounds,
WAL-only commits, concurrent atomic updates, insertion/physical-layout changes,
inactive checkpoint preservation and cancellation. An independent Python SQLite
encoder reproduces empty and populated Go vectors without using the Go helpers.

Structural validation does not validate signatures, command semantics, canonical
chain evidence, current budgets or historical completeness. The account lane
still overwrites older signed bytes. A digest cannot reconstruct lost rows or
detect a missing newer valid state without an independent authenticated fresh tip.
Durable independent authority transport, semantic mutation/replay validation,
atomic applied-sequence tracking, custody fencing and the pre-signing boundary
remain required before integration or any recovery-availability claim.
