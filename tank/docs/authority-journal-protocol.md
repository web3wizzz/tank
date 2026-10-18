# Experimental authority journal contract

`internal/authority` is an offline protocol foundation for the
[recovery design](disaster-recovery-design.md), not an operating backup service.
No coordinator, metadata writer, signer, backup command or restore controller calls
it. The memory backend exists only in `_test.go`; it cannot be selected by a binary.
Existing restore gates, databases, schema, signing journals and budgets are unchanged.

## Integrity format

Version 1 binds an independently trusted genesis anchor: stream ID, schema version,
release digest and digest of the **complete** initial authoritative state. Existing
registration-only `RestoreSnapshot` is insufficient for that initial state: identity,
credentials/revocations, grants, ownership and file metadata are also required.

Each immutable request binds that anchor, exact preceding tip (sequence, epoch,
digest), writer epoch, unique operation ID, mutation kind, complete private payload,
and resulting state digest. Its record advances the sequence by exactly one.
IDs/kinds are 1–128 ASCII letters, digits, dots, underscores or hyphens; payloads
are 1 byte–1 MiB. The payload remains opaque here: this package cannot validate a
revocation, reservation, signed transaction or resulting state digest semantically.
Larger mutations need a reviewed chunk/transaction protocol, not a larger limit
silently introduced by an adapter.

Digests are SHA-256 of `domain + NUL + encoding/json` of the fixed Go structs.
Domains are `tank-authority-genesis-v1` and `tank-authority-record-v1`.
Exported field names and declaration order form part of the versioned format;
byte slices use base64 and 32-byte digests use arrays of byte numbers. No maps,
floating values or user-supplied JSON participate in the framing. Changing framing
requires a new version and migration review. Tests pin genesis/record vectors,
independently reproduced with Python:

```text
genesis be0b4f607cc624119ccc6778d7caf9fc6d2087fa908280e5ec3657265e9901d0
record  68b7102a9239fdc654b0ec5c68523fd73667b69eac2c031ee14e7b9166f331fa
```

`Prepare` computes a candidate, never an acknowledgement. `Verify` checks a full
genesis-to-tip prefix, including anchors, contiguous sequencing, previous digests,
monotone epochs, unique operation IDs and exact terminal tip. It applies no state
and opens no database. Acquisition/decoding must be bounded before passing records;
verification memory grows with operation count, and cancellation is checked between
bounded records. Snapshot-based streaming replay remains future work.

## Independent authority transport requirements

The `Journal` interface requires linearizable current-tip, append, lookup and bounded
read operations. A future adapter must implement all of these requirements:

1. Authenticate a **fresh** current-tip query independently of local files, saved
   receipts and the replayed journal. Replay protection must bind a fresh challenge
   or equivalent authenticated protocol to the stream/anchor and current position.
   A historical valid signed receipt is not a current-tip proof.
2. Validate mutation semantics against the preceding committed state, enforce an
   independently granted active writer epoch and compare the full expected tip.
   The integrity package cannot perform these operations for an adapter.
3. Deduplicate operation IDs **before** comparing the current tip/epoch. Retrying
   the byte-identical original request returns its original receipt even after
   later commits or an epoch change. A changed request using the ID conflicts.
4. Acknowledge only after independent durable persistence. Resolve an unknown
   outcome by resubmitting the identical request or looking up its accepted
   receipt. A not-found lookup cannot cancel an in-flight append or prove it will
   never commit. Never allocate a replacement operation ID, nonce or budget charge
   merely because a response was lost. Treat any result without definite rejection
   as uncertain; stop downstream mutation/signing until resolved.
5. Bound reads/transport work, honor deadlines, copy mutable payload buffers at
   ownership boundaries and return sanitized errors. Do not log private requests,
   signed bytes, credential material, digests or raw backend error details.

`Verify` cannot establish that the tip supplied by its caller is fresh: an old
valid prefix supplied with its old valid tip passes integrity verification. A test
explicitly preserves this limitation. Neither hash chaining nor this interface
authenticates an independent host or provides rollback protection by itself.

## Local evidence and remaining implementation

Run `go test -race -count=1 ./internal/authority`. Synthetic tests cover concurrent
compare-and-append, failure before persistence, committed append with lost response,
exact retries after subsequent commits/epoch changes, conflicting IDs, storage-epoch
rejection, partitions, cancellation, buffer aliasing and damaged/gapped/truncated
history. Toy replay after losing local state preserves a synthetic revocation,
permanent reservation and immutable byte string; it is not a production metadata
or signer recovery test, disk durability test or independent-host measurement.

Next: inventory and cover **all** metadata mutation paths; define complete-state
bootstrap/digest and deterministic semantic command replay; implement the custody
intent/byte-persistence boundary; test crashes between remote commit, local apply
and response. Initial migration must import existing historical bytes, permanent
reservations and revocations without resetting authority. Do not wire a partial
writer into live signing or claim complete recovery from registration-only state.

Production also requires selected independent storage/host custody, authenticated
tip transport, enforceable writer grants and custody fencing. A storage epoch
cannot stop an old local keystore from signing or broadcasting retained bytes.
No automatic failover, promotion or signing recovery is enabled by this package.
Loss of newest authority remains fail-closed under the existing controller.
