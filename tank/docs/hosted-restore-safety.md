# Failure and restore safety

Remote failure execution and restore activation remain disabled. The read-only
hosted preflight now has `--dry-run`; local orchestration is implemented in
`scripts/pilot-restore.py`, and canonical reconciliation in `tank-restore-check`.
These safeguards can be validated without independent hosts. They do not provide
hosted availability evidence or automatically move a production service to a copy.

## Shared signing lane

Both production Arc CLIs acquire a nonblocking exclusive filesystem lock **before
opening custody**. `TANK_REGISTRATION_LOCK_PATH` selects the one stable absolute
lock path; if omitted, the existing `TANK_DATABASE_PATH` plus
`.registration.lock` is used. Deployment, registration, and restore must use the
same path on the same host, service identity, and persistent filesystem. The
systemd templates already grant write access to the coordinator state directory.
Separate hosts or copied lock files do not coordinate signing; the pilot remains
one account and one signing host. Do not launch an alternative signer tool.

The lock inode is retained permanently. Never remove or replace it to unlock a
worker. An active worker causes controller rejection; the controller does not
kill a signing operation. Job leases continue protecting database updates, while
the process lock prevents another worker from reaching the signer first.

`LOCK_PATH.restore-gate` blocks both CLIs after interruption even after the OS
releases the lock. `DATABASE_PATH.restore-gate` additionally blocks a restore
candidate regardless of its selected lock path. Never manually delete a gate to
bypass failed reconciliation. Actual private databases, gates, controller journals,
deployment journals, credentials, signed bytes and backups stay out of Git/logs.

## Local controller

The controller accepts only a private directory with an explicit
`.tank-drill-fixture` marker. All databases, deployment journals, validator binary,
controller journal and lock must be below that directory. It requires exact
owned local process identities: PID, Linux start ticks, command digest, and fixture
working directory. Roles identify one coordinator, one registration timer, and
storage nodes; `failure_targets` selects only declared node PIDs. It never accepts
remote commands, SSH execution, arbitrary stop commands, or a signing-worker PID.
The selected owned node processes are paused locally; this simulates process
loss, not independent-machine loss or spare repair acceptance.

The controller:

1. Acquires the shared lane, writes/fsyncs a maintenance gate and private controller
   journal, then pauses the declared timer, coordinator and selected node targets.
   Linux PID descriptors prevent a recycled PID from receiving signals.
2. Holds a SQLite write reservation on the live DB. This blocks all metadata
   writers, including credential administration. It creates a consistent snapshot
   if a new snapshot path was selected, or reads an existing chosen snapshot.
3. Compares schema and every logical table row against the frozen live DB, including
   manifests, access grants, revoked/expired credentials, completed registrations,
   immutable signed bytes, nonce lanes and cumulative spend history. **Any stale
   backup is rejected**; no guessed merge or rollback is attempted.
4. Publishes the restore copy at a fresh path without replacement, keeps that copy
   gated, and holds its write reservation during reconciliation. The live DB,
   snapshot and deployment journals remain intact.
5. Runs the pinned validator binary by open file descriptor with a bounded timeout
   and suppressed child diagnostics. It verifies unchanged source/candidate state
   and deployment journal digests afterwards. Only a verified, unchanged original
   live lane can be released; the candidate remains inactive and signing-blocked.
6. Resumes owned coordinator/node processes in cleanup. A failed/interrupted run
   leaves the timer paused and signing gated. The same private controller journal
   resumes with `--resume`; it rechecks identities, state, binary and chain rather
   than replacing a candidate or trusting a previous incomplete phase.

No service switch to a restored database, journal reset, credential resurrection,
automatic deletion, fee replacement or duplicate registration is authorized by this
controller. Old backups cannot currently recover a lost newest live journal:
absence of that comparison baseline fails closed. A future disaster-recovery merge
needs separately reviewed authoritative accounting/revocation history.

## Canonical chain reconciliation

Build the read-only command from `tank/`:

```bash
go build -trimpath -o bin/tank-restore-check ./cmd/tank-restore-check
```

Load the existing private worker environment in a local terminal without tracing
or printing it; reuse its RPC, registry, account and artifact configuration. The
validator never reads the keystore/password or constructs a signer:

```bash
./bin/tank-restore-check --db /ABSOLUTE/PRIVATE/RESTORE_CANDIDATE.sqlite
```

It checks chain 5042002 and exact compiled registry runtime through the existing
HTTPS reader. The production command rejects a registry or sender other than the
published pilot identity. Every historical registration hash requires confirmed transaction
bytes whose signature, sender, target, calldata, nonce and approved fee policy
match its manifest. Receipts and commitment/size readback use the existing canonical
block-hash checks. Exact signed `gasLimit * maxFeePerGas` must equal its permanent
reservation; actual receipt cost must fit that reservation and transaction limits.
Reservation totals must equal the durable account counter and the unchanged
0.10 testnet USDC budget. The latest chain bytes must match the persisted bytes.

For this already-deployed dedicated account, deployment used nonce 0; registration
history must contain the complete unique nonce sequence 1 through N, with both
latest and pending account nonce N+1. Empty/unsupported histories, missing or
ambiguous receipts, pending transactions, external nonce consumption, inconsistent
budgets and stale backups keep signing blocked. Pending signed bytes are retained;
this validator never calls the worker's broadcast/retry path to resolve ambiguity.
The existing worker can retry unchanged bytes after maintenance has been safely
resolved. The schema retains the latest lane bytes; older bytes are read from the
confirmed chain and verified against their permanent reservations, not fabricated.

Approved ceilings remain 65 Gwei, 0.008 testnet USDC per registration, 0.10 aggregate,
and 0.04 deployment. No funding or policy increase is part of reconciliation.

## Reproducible isolated checks

```bash
python3 -W error::ResourceWarning scripts/test-pilot-restore.py
python3 scripts/test-pilot-hosted-tests.py
go test -race ./internal/registrationguard ./internal/metadata ./internal/registry ./cmd/tank-prepare-registration ./cmd/tank-restore-check
TANK_ARC_FORGE_BIN=/PATH/TO/PINNED/arc-forge TANK_ARC_ANVIL_BIN=/PATH/TO/PINNED/arc-anvil bash scripts/test-arc-local.sh
```

Python fixtures generate private disposable databases and real owned local
processes. Their validator outcomes are injected, not chain evidence. Go tests
cover cryptographic/history rejection and real loopback Arc/TLS/SQLite snapshot,
unchanged retry, one signature/submission and no duplicate registration. Every
temporary artifact belongs to a fixture; existing user data is never deleted.

For manually created isolated fixtures, copy
`deploy/pilot/local-restore.example.json` to a private mode-0600 file and replace
its paths and exact owned-process identities locally. Keep the validator SHA-256
and credentials/RPC environment local:

```bash
python3 scripts/pilot-restore.py --config /PRIVATE/fixture/config.json --dry-run
python3 scripts/pilot-restore.py --config /PRIVATE/fixture/config.json --execute-local-fixture
python3 scripts/pilot-restore.py --config /PRIVATE/fixture/config.json --execute-local-fixture --resume
```

Before remote execution can be enabled: select independent hosts/operator access;
review ownership, timer/worker stop-and-quiesce controls, stable signing-lane storage,
and safe candidate activation; connect browser file-ID/registration evidence and
full node-loss/spare-repair controls. Run fresh host-specific acceptance and backup
measurements. Local success does not satisfy these external gates.
