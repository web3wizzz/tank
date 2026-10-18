# Tank hosted testnet pilot preparation — unreleased

This note describes the current repository state, not a public hosted service
release. The TankRegistry deployment and one signed registration are verified on
Arc Testnet. The browser workspace, coordinator, and storage acceptance still
runs on local owned processes; no independently hosted pilot is claimed.

## Available

- Browser-side encryption and filename protection, recovery-key export/import,
  integrity-checked retrieval, user isolation, and a responsive workspace.
- [Opt-in CLI/SDK encryption](../sdk-encryption.md) with the browser-compatible
  envelope, private recovery export/import and real Go/TypeScript/browser
  interoperability checks. Raw-byte APIs and stored-byte IDs retain their semantics.
- Four-data/two-parity shard encoding, healthy-spare uploads across three declared
  failure domains, verified spare repair, authenticated operations reports and
  [replicated private SQLite checkpoints](../signing-state-recovery.md).
- Arc Testnet registry and public signed-registration evidence at
  [`arc-testnet-acceptance.md`](../arc-testnet-acceptance.md).
- Hardened systemd templates for coordinator, node, frontend, and a bounded Arc
  registration timer. These templates have not been installed on remote hosts.
- Shared signing-lane locks and persistent restore gates; a local-only controller
  rejects stale snapshots, preserves revocations/accounting and resumes interrupted
  verification. Read-only Arc history reconciliation verifies signed intents,
  canonical receipts/readback, exact reservations and nonce ownership. Restore
  candidates remain inactive; remote failure/activation execution stays disabled.
- Correlated browser control evidence and phase ordering, with credentials and
  recovery keys excluded. The chainless fixture stays `not_queued`; opt-in local
  container CI verifies queued/restart registration on chain 31337, exact encrypted
  recovery, isolation and persistence. Neither provides public Arc signing proof.
- Node capacity parsing rejects unknown/null counters and prevents case aliases
  overriding validated limits. CLI inputs and compiled-artifact inputs reject
  named pipes without waiting for writers; CLI API responses require one complete
  JSON value within the existing limit.
- Existing-authority access administration refuses missing/uninitialized databases;
  explicit user creation retains bootstrap. Credential publication flushes new
  directory entries before success and retains new-key-only revocation on failure.
- An [offline authority journal contract](../authority-journal-protocol.md) and
  [complete mutation inventory](../authority-mutation-map.md) support recovery
  design/testing. They do not implement independently durable authority, fresh-tip
  authentication, custody fencing or signing recovery after newest-state loss.

## Verification

The local pilot browser drill uses synthetic files and temporary loopback
processes. It covers exact local decryption, user isolation, process loss, spare
repair, metadata restore, restart, and threshold outage/recovery. It does not
prove independent host failure, sustained uptime, or a service-level commitment.
See [`pilot-operations.md`](../pilot-operations.md) for reproducible checks and
the hosted acceptance runner's current boundary.

## Before inviting users

Select and verify independent hosts, stable HTTPS endpoints, operator custody,
monitoring and off-machine backup access. Complete a separately reviewed hosted
failure/restore drill and measure capacity and recovery on those hosts. Resolve
the draft [retention, funding, and operator-exit decisions](../pilot-owner-decisions.md).
No retention term, customer quote, performance guarantee, or production
availability is approved by this note.
