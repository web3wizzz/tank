# Offline signed-registration preparation

The new internal preparation/journal layer does not select a wallet, connect to
an RPC, submit a transaction, or enable the public registration worker. The
existing local unlocked-account client stays restricted to Anvil chain31337.
A selected custody and RPC adapter are required for a future authorized pilot.

## What is implemented locally

`registry.PrepareTestnetRegistration` accepts a claimed durable registration job,
a manifest in metadata, an explicit policy, transaction terms, and a caller-owned
`TransactionSigner` interface. It constructs only the Tank registry call on Base
Sepolia chain84532, with zero transferred value and explicit gas/fee caps. The
signer must honor cancellation and return a signature without broadcasting.

The layer seals the authorized intent hash before calling the signer. It then
serializes and decodes a fresh transaction snapshot before recovering the sender
and checking that every signed field matches. This rejects calldata mutation,
wrong accounts/chains, altered fees/gas/nonce/value/destination/access lists,
and cached sender recovery with a subsequently corrupted signature. Signer
errors are redacted; no key or provider error enters metadata or logs.

Before any future caller can submit, SQLite atomically stores the signed bytes,
nonce, and locally calculated transaction hash under the current job fence and
an account lane keyed by chain and signer, independently of registry contract.
Only one transaction can remain outstanding per account. Expired worker leases
never free that lane. Restart resumes the same bytes without asking for a new
signature or allocating a new nonce. A confirmed nonce remains recorded; only
its immediate successor is accepted. Nonce discrepancies require review.

`registry.ConfirmPreparedRegistration` checks a caller-supplied observation:
successful receipt, correct transaction hash, canonical receipt-block header,
configured confirmation depth, and matching commitment/size read at that same
block. Stale job tokens cannot complete the lane. Reverted/missing receipts,
reorgs, insufficient depth, unqualified latest-state readback, and conflicting
records leave the intent outstanding.

The observation is trusted input from the future RPC adapter; this is not an
Ethereum light client or a cryptographic proof of chain finality. L2 confirmation
depth is distinct from L1 batch finality. See [Base finality](https://docs.base.org/specifications/transactions/transaction-finality).

## Fee policy boundaries

All preparation bounds are explicit integer values: gas, maximum execution fee,
maximum priority fee, and `MaxExecutionCostWei` for `gas × maxFeePerGas`.
Negative/oversized values, estimates over caps, and arithmetic overflow-sized
inputs are rejected before contacting the signer. No cap is raised automatically.

This execution budget does **not** include Base's additional L1/security fee.
The future broadcaster must include those fees and balance/spend reservations
under the operator's approved total policy. See [official Base fees](https://docs.base.org/specifications/transactions/network-fees).
No aggregate wallet spending budget, fee replacement, or public broadcaster is
implemented; preparation must not be presented as a complete cost guarantee.

## State custody and upgrades

The journal is an additive schema7 migration. Upgrade metadata users together,
using `tank-backup` before changing an important database. Snapshot reads do not
migrate the source. Existing manifests, users, credentials, permissions, names,
repair jobs, and registration jobs are retained. A schema6-to7 test verifies this.

Signed transaction bytes contain no private signing or browser recovery key, but
any holder can submit them. Treat the database and its backup as private
operational state; never dump, commit, publish, or attach the raw journal to logs
or CI artifacts. Completed nonce history survives restart and private backup.

## Validation and remaining integration

Offline tests use synthetic in-memory signers and owned SQLite databases. They
exercise signer mutation/cache attacks, exclusive lanes across two workers and
contracts, crash/reopen and expired leases, budget checks, malformed state,
confirmation/reorg/readback failures, stale completion, and nonce reuse/skip.
The browser pilot drill separately verifies encrypted file/authorization recovery
across machine-process loss and metadata snapshot restoration.

Remaining public integration requires the selected dedicated signer/custody,
chain-bound RPC adapter, immutable-byte rebroadcast with ambiguous-error handling,
nonce discrepancy reconciliation, Base total-fee accounting, canonical block
queries, and explicit broadcast authorization. Do not activate a public worker
by removing restrictions from the existing local client.
