# Signed registration preparation and authorized Arc Testnet submission

The preparation/journal layer defaults to signing and observation without
submission. Explicit `tank-prepare-registration --broadcast` now enables the
guarded Arc Testnet submitter after private configuration and exact runtime
verification. See [deployment and setup](arc-testnet-deployment.md). The
existing local unlocked-account client stays restricted to Anvil chain31337.
An offline encrypted-keystore custody adapter and read-only RPC adapter are
available. Operator custody selection and public-provider acceptance are still
required for the pilot; testnet-only broadcast authorization has been supplied.

## What is implemented locally

`registry.PrepareTestnetRegistration` accepts a claimed durable registration job,
a manifest in metadata, an explicit policy, transaction terms, and a caller-owned
`TransactionSigner` interface. It constructs only the Tank registry call on explicitly configured Arc Testnet
chain 5042002 (or isolated local chain 31337), with zero transferred value and explicit gas/fee caps. The
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
Ethereum light client or a cryptographic proof of chain finality. Arc finalizes
on inclusion; use one canonical confirmation.

## Fee policy boundaries

All preparation bounds are explicit integer values: gas, maximum execution fee,
maximum priority fee, and `MaxExecutionCostWei` for `gas × maxFeePerGas`.
Negative/oversized values, estimates over caps, and arithmetic overflow-sized
inputs are rejected before contacting the signer. No cap is raised automatically.

Arc native fee values use 18-decimal USDC base units. The policy enforces the
20 Gwei fee floor; total execution bounds and balance/spend reservations must
remain within operator limits. Arc has no Base L1/security fee component.
Authorized mode reserves every transaction ceiling in a durable schema8
aggregate registration budget before submission. Retries charge the same hash
once. Deployment has a separate cap. Fee replacement is not implemented; other
journals/tools must not share the dedicated account.

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

The `TestnetReader` adapter uses a selected HTTPS provider, checks chain5042002
and code at the chosen registry, and exposes only pending nonce and canonical
observations. Responses and time are bounded, redirects are refused, provider
errors are redacted, and the chain is rechecked for every workflow. Registry
readback uses a block hash with `requireCanonical=true`, followed by another
canonical-header check. No service activates it automatically; tests use owned
TLS fixtures. It checks code presence, not a deployed-bytecode fingerprint.

## Secure custody and preparation worker

`KeystoreSigner` accepts an encrypted Ethereum V3 keystore and a password file.
Both files must be regular, owned by the process user, and inaccessible to group
and others (mode 0600); symlinks are rejected. Use a private mode0700 directory
outside git. It verifies the expected public address, refuses the first standard
Anvil account, and accepts only zero-value type-2 Arc Testnet contract calls.
Operators must create a dedicated account: other known development keys are not
an acceptable source of custody. Keys remain in process memory until closed;
this adapter is not a hardware signer. Errors omit paths, passwords and keys.

`tank-prepare-registration` is a bounded, manually invoked preparation command.
It is not started by the coordinator or the local launcher. After private
configuration is supplied, it estimates fees/gas, checks native USDC balance,
signs and journals a registration intent, and checks canonical observations.
`SignedWorker` retries with bounded backoff; absent/reverted receipts preserve the
same signed bytes and account lane. Restart reconciles persisted transactions
without requesting another signature or estimate. Default mode never submits
transactions; `--broadcast` uses the guarded submitter.

Build it with `go build -o bin/tank-prepare-registration ./cmd/tank-prepare-registration`.
Use the placeholder names in `deploy/arc-testnet/testnet.conf.example`, configured
through the operator's private environment. Do not run against a remote provider
until those inputs have been securely supplied. The command requires an existing
owned private parent directory for `TANK_DATABASE_PATH`, refuses symlink database
and WAL/SHM files, and prints only a generic completion result. Keep backups in
private directories too; signed bytes are broadcastable operational state.

The reader uses twice the current suggested gas price (at least the Arc floor)
and a 20% gas margin within the explicit caps, then checks that native balance
covers `gasLimit * maxFeePerGas`. This checks one intent, not aggregate spending.
Fee/balance/provider failures stop preparation before signing.

Public acceptance still requires selected dedicated custody, working RPC, faucet
funding and an encrypted-file pilot run. Authorized submission and immutable-byte
retry/restart handling plus aggregate spend reservations are implemented and
verified locally. A remote signer service has not been selected or integrated.
Do not activate a public worker by removing restrictions
from the existing local client.
