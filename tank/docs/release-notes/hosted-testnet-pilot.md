# Tank hosted testnet pilot preparation — unreleased

This note describes the current repository state, not a public hosted service
release. The TankRegistry deployment and one signed registration are verified on
Arc Testnet. The browser workspace, coordinator, and storage acceptance still
runs on local owned processes; no independently hosted pilot is claimed.

## Available

- Browser-side encryption and filename protection, recovery-key export/import,
  integrity-checked retrieval, user isolation, and a responsive workspace.
- Four-data/two-parity shard encoding, three-node initial placement, spare repair,
  authenticated operations reports, and private SQLite backup/restore tooling.
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
  recovery keys excluded. The local status fixture stays `not_queued` and supplies
  no fabricated public-chain confirmation.

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
