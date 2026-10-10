# Tank Phase 1 development status

Status: Phase 1 complete; Phase 2 local preparation verified. Real testnet pilot
acceptance remains blocked by the deployment/custody/retention inputs below.

Scope: complete the local MVP backlog below. Verified milestones may be committed
and pushed to the existing origin; no deployments, force pushes, secret exposure,
spending, or destructive changes to existing local data are authorized.

## Completed

### Browser encryption and recovery

- Present in `ac56aa7` (already on origin/main when Phase 1 publishing began).
- Fixed the Codespaces localhost Origin rewrite with exact frontend-origin and
  forwarding checks; the exact origin is persisted in private local configuration.
- Verified the real private Codespaces HTTPS URL: sign-in, encrypted PDF upload,
  recovery-key import, original Unicode filename and identical bytes, wrong-key
  rejection, cross-user isolation, and logout.
- Validation: Go race tests, vet/build, 15 TypeScript SDK tests, 17 frontend tests,
  lint/build, five launcher tests, and isolated browser integration including node
  loss and revocation. No local data or existing credentials were removed.

### Authentication and account continuity — `d8183a6`

- Issuing a replacement credential retains the existing user's identity and file
  permissions. Expired/revoked credentials do not prevent local administrative
  recovery; unrelated users retain no access.
- Added local `users`, `keys`, and `issue` commands. Listings omit tokens/hashes;
  writes are private and refuse existing destinations and symlinks.
- Validation: metadata/CLI regression tests, full Go race/vet/build checks,
  frontend tests/lint/build, and browser recovery after credential replacement.
- Pushed to origin/main. Tank CI and TypeScript SDK CI both passed.

### Browser file-list pagination — `6fee218`

- The workspace now fetches later pages and rejects malformed cursors. New uploads
  no longer discard previously loaded file IDs; appended pages are deduplicated.
- Verified all 101 stored files across pages, user isolation, encryption/recovery,
  replacement credentials, and retrieval after stopping an owned test node.
- Validation: frontend production build, lint, 17 unit tests, full isolated browser
  integration, and diff checks passed. Pushed to origin/main; Tank CI and
  TypeScript SDK CI passed.

### Authentication/session hardening — `96d1cd6`

- Validates sessions upstream before buffering upload bodies.
- Cancels workspace requests at session end/unmount and checks cancellation before
  downloads after asynchronous crypto operations.
- Browser regressions passed for a headers-only unauthenticated upload and a held
  authenticated retrieval followed by logout, with no late plaintext download.
- Validation: 17 frontend tests, lint/build, full isolated integration, diff checks.
- Pushed to origin/main. Tank CI and TypeScript SDK CI passed.

## Completed milestone: resource limits and quotas

- Validated configuration for upload size, headers, request deadlines, authenticated
  rate budgets, request concurrency, and accepted TCP connections.
- Logical per-user/total storage quotas check before shards are written, protected
  by a fenced SQLite upload lease shared across coordinator processes.
- Node capacity counts retained data and supports repair/atomic replacement without
  deleting existing files. Frontend admission bounds buffered requests and returns
  stable 413/429/507 errors. Integration settings are explicitly isolated.
- Validation passed: full Go race tests, vet/build, SDK tests/build, frontend
  19 tests/lint/build, and isolated browser integration. A stalled registration
  regression verifies the configured deadline and admission release.
- Security review found no confirmed quota race; authenticated governor buckets
  persist for administratively provisioned users. Published as `aaff72f`; Go and SDK Actions passed.
- Independent review findings retained for upcoming frontend/docs/CI milestones:
  mobile overflow, stale FAQ, same-file reselection, login error/focus handling,
  contract CI, and broader SDK/build coverage.

## Completed milestone: frontend usability

- Fixed 320px navigation overflow, repeated file selection, stale encryption FAQ,
  robust non-JSON sign-in errors, focus after sign-in/logout/expiry, and upload
  guidance using the configured encrypted-file size bound.
- Frontend 19 tests, lint, production build, and isolated integration passed.
  Browser checks cover 320/375/768px signed-in/out layouts, keyboard submission,
  HTML gateway failures, repeat selection, and focus restoration.
- Published as `6b8b818`; Go and SDK Actions passed.

## Completed milestone: documentation

- Updated README, CONTRIBUTING, Go/TypeScript SDK guides, and local-MVP checks
  for exact-origin browser setup, per-user credentials/renewal, encryption,
  pagination, quotas, cancellation, and repeatable validation.
- Added SECURITY.md with private reporting, trust boundaries, copied-session
  replay limits, key loss, local-chain restrictions, and private-state handling.
- Reviewed examples against current APIs; all local documentation links pass.
  Diff/private-artifact review passed. Published as `9b6ef73`; Go and SDK Actions passed.
- Subsequent CI milestone completed below.

## Completed milestone: CI coverage

- Added browser tests/lint/build and isolated Go/browser integration on pushes/PRs.
- Added contract formatting/build/tests with Foundry v1.8.4 and a pinned installer
  action; all 8 local contract tests passed, including 256 fuzz cases.
- Go formatting now includes public SDK/examples and builds every executable.
  SDK CI runs for feature-branch pushes as well as main.
- actionlint v1.7.12 passed for all four workflows; launcher 5 tests and broader
  Go builds passed. Published as `5e95606`; Go, SDK, browser, and contract Actions
  all passed.
- Subsequent final review completed below.

## Completed milestone: complete MVP review

- Added isolated JSON-RPC/SQLite tests for worker configuration, 101-file
  reconciliation preserving completed jobs, wrong chain/missing contract,
  matching/conflicting commitments, RPC outages, durable transaction persistence,
  missing readback, and recovery without duplicate submission.
- Reproduced and fixed case-sensitive localhost validation in the registration
  client, aligning it with worker validation. New regression fails before the fix
  and passes after it. Public/non-loopback RPC remains rejected.
- Go example output now goes to ignored `bin/examples/` in CI.
- Validation: full Go race tests/vet/build/formatting, focused final registry race
  tests/vet/build, 15 SDK tests/typecheck/build, 19 frontend tests/lint/build,
  launcher 5 tests, contract 8 tests (256 fuzz cases), actionlint, and isolated
  browser integration passed. Production npm audits reported no vulnerabilities.
- Repeated the updated browser workflow through the actual private Codespaces
  HTTPS gateway: sign-in, encrypted PDF, recovery import, original Unicode
  filename/exact bytes, wrong/missing key rejection, cross-user isolation, mobile
  layout, repeated file selection, and logout/focus all passed.
- Found the previous live coordinator/nodes/Anvil stopped. Created private online
  SQLite and chain snapshot backups, rebuilt all tools, and restored local
  services. Verified schema 6 retains the original manifests, identity,
  credentials, permissions, and filename records. Existing logs were retained;
  restored services write separate ignored private logs.
- Published as `7359f95`. All four GitHub Actions workflows passed:
  [Go](https://github.com/web3wizzz/tank/actions/runs/38016499236),
  [SDK](https://github.com/web3wizzz/tank/actions/runs/38016499168),
  [contracts](https://github.com/web3wizzz/tank/actions/runs/38016499167), and
  [browser integration](https://github.com/web3wizzz/tank/actions/runs/38016499199).

## Blockers and next task

Phase 1 is complete and published, with all verification passing. The user has
authorized Phase 2 local preparation below. Actual remote deployment/broadcast,
paid infrastructure, payments, signup, streaming, rollup DA, and package
publication remain outside current authorization.

Known MVP limits are documented in SECURITY.md and the resource/browser guides:
lost recovery keys cannot be recovered; copied sessions survive logout until
expiry or credential revocation; failed uploads may retain shards; node capacity
assumes one process owns each directory; chain resets can stale recorded status.

Private credentials, environment, backups, local storage, chain state, logs,
dependencies, and build output remain ignored and untracked. Restored local
services remain available; no deployment or existing-data deletion occurred.


## Phase 2: reliable testnet pilot preparation

User-authorized scope: configurable coordinator/four-node deployment templates,
local machine-failure and backup/restart recovery drills, operator documentation,
and Base Sepolia deployment scripts with placeholders. No remote provisioning,
paid infrastructure, testnet broadcast, or deployment is authorized yet.

Existing quotas and audit/repair scheduling will be reused. Independent reviews
confirmed gaps in backups, authenticated monitoring, remote transport, per-node
credentials, network-specific registration display, and public-chain signing.
The local unlocked Anvil client must not be repurposed for public RPC merely by
removing its restrictions.

### Completed milestone: private metadata backup/restore — `0a0c8d4`

- Added online SQLite snapshots and `tank-backup`, with integrity/foreign-key
  verification, mode-0600 output, atomic no-overwrite publication, source schema
  preservation, and bounded deadlines.
- Tests cover live WAL/concurrent writes, restored users/credentials/permissions,
  filenames, manifests, durable jobs, cross-user isolation, destination refusal,
  cancellation, and snapshot independence. Full Go race tests/vet/build passed.
- Fixed SQLite path URI encoding so snapshots containing query punctuation
  reopen as the intended file; regression verified. All four Actions passed.
- Added backup/restore guide, including revocation reconciliation and separate
  node-shard/user-key custody.

### Phase 2 blockers and next tasks

- Real hosts, failure domains, RPC endpoint, signer/custody, pilot users, and
  deployment authorization are not selected. Do not invent them or broadcast.
- Base Sepolia signed registration requires chain binding, bounded fees,
  pre-broadcast durable transactions, nonce ownership, confirmations, and RPC
  error redaction. Local Anvil validation remains separate.
- Next: authenticated operational monitoring; remote-node TLS/credential setup;
  deployment templates/operator retention promise; reproducible local failure,
  restart/backup restore acceptance; offline Base Sepolia preparation scripts.


### Completed milestone: authenticated operational status — `d3df72a`

- Added administrator-only coordinator and node-credential-only storage reports.
  Aggregates expose logical/physical capacity, repair/registration backlog, and
  last audit execution, with bounded parallel node probes and no private labels.
- Liveness remains separate; reports indicate degraded dependencies and upload
  readiness without promising every stored file is available.
- Go race tests for metadata/storage/nodes/coordinator, vet/build, and diff checks
  passed. Regression tests cover role isolation, exhausted quotas, closed
  backends/database, stalled probes, and malformed capacity responses.
- Added monitoring guide. All four Actions passed.
- Next: TLS and per-node credentials, deployment templates, and failure/restore
  browser integration; Base Sepolia scripts remain preparation-only.


### Completed milestone: separate-machine configuration preparation — `4560107`

- Remote nodes require HTTPS; loopback HTTP stays compatible. TLS trust and
  redirect rejection tests pass. No insecure-certificate option was added.
- Coordinator supports a private per-node credential map with preserved placement
  order, complete coverage, unique tokens, and private-file/symlink checks.
- Added configurable coordinator/node/frontend templates, hardened systemd units,
  reverse-proxy examples, and hardware/ports/storage/secret/operator documentation.
- Retention duration/funding/operator-exit terms are explicitly undecided and
  required before real users; no perpetual-storage promise is made.
- Full Go race tests/vet/build passed; local systemd template validation and
  configuration/secret placeholder checks passed. All four Actions passed.
- Next: reproducible local machine-loss, repair, metadata restart/restore, and
  encrypted browser acceptance; non-broadcast Base Sepolia preparation.


### Completed milestone: reproducible local pilot acceptance — `a50aa6b`

- Extended the owned temporary deployment to use four distinct node credentials.
- Verified encrypted PDF retrieval after active-node process loss, automatic
  spare repair after coordinator restart, private metadata snapshot/restore,
  and loss of a second original node. Exact bytes/Unicode filename and user
  isolation survive; revocation/renewal work against restored metadata.
- Further node loss exceeds coding tolerance: download is blocked with a stable
  retryable error, then restarting the preserved node permits exact decryption.
- Fixed the maintenance worker's unnecessary five-second delay per ready job.
  A regression proves queued jobs drain serially before the polling interval.
- Coordinator race tests/vet/build and frontend 19 tests/lint/production build
  plus the full pilot drill passed. Browser CI now runs the stronger drill.
- Evidence is explicitly local process-loss emulation, not independent machines
  or testnet registration. All four Actions passed.
- Next: offline Base Sepolia preparation/contract validation and readiness docs;
  real RPC/signer/hosts and deployment authorization remain absent.


### Completed milestone: Base Sepolia preparation and pilot handoff — `70a31cd`

- Added a Foundry deployment script bound to chain 84532 and a nonzero selected
  public sender. Offline tests simulate creation and reject wrong-chain/zero
  sender configurations, without a network or private wallet.
- Added unsigned bytecode plans, placeholder RPC/signer configuration, and an
  optional HTTPS read-only eth_chainId preflight with redirects refused and
  provider errors redacted. The preparation CLI rejects broadcast/signing args.
- Contract formatting/build and 11 tests passed (including 256 fuzz runs); three
  Python preparation tests and an actual unsigned bytecode-plan check passed.
- Operator guides describe private TLS/storage/monitoring/backups, hardware sizing
  assumptions, retention funding/operator exit, and local versus real evidence.
- All four Actions passed. Real pilot acceptance is still blocked by absent
  host/RPC/signer/user decisions, signed-worker integration, and deployment
  authorization. No real testnet or remote deployment has occurred.


### Completed milestone: offline signed-registration journal — `8023ab3`

- Added caller-provided signer intent preparation with fixed testnet chain,
  zero-value registry calls, explicit execution gas/fee/budget bounds, and no RPC
  transport or broadcaster. No real signing credential is selected.
- Sealed intent before custody callbacks and validate fresh serialized snapshots;
  malicious-signer tests caught and fixed calldata mutation and sender-cache/
  signature corruption bypasses before publication.
- Additive schema7 journal atomically fences jobs and one outstanding signer
  account transaction across contracts/workers. Restart resumes immutable bytes;
  expiry never frees the lane; confirmed nonce history prevents reuse/skipping.
- Confirmation checks require receipt success, canonical hash/depth, pinned
  readback, matching commitment/size, and current fencing. These are trusted-RPC
  policy checks, not L1 finality proofs. Execution fee caps exclude Base L1 fees;
  total fee/spend policy remains required for any future broadcaster.
- Full Go race tests/vet/build and focused security regressions passed; final
  schema7 browser restore validation passed. All four Actions passed.
- Next: finish verification and publish; actual pilot remains blocked by selected
  hosts/RPC/custody/funding/retention terms, adapter/broadcaster integration, and
  deployment authorization. Phase3–5 work remains outside this milestone.


### Completed milestone: read-only testnet observation adapter — `4199d14`

- Added an HTTPS-only, chain84532-bound reader with registry-code presence checks,
  bounded response/time, redirects refused, and provider errors redacted.
- It reads pending signer nonce, successful receipts, canonical headers, and
  hash-pinned registry records with requireCanonical=true; it rechecks canonical
  block identity after readback and chain identity for each workflow.
- No signer, wallet, submission API, default RPC, or automatic activation exists.
  Real configuration inputs remain unselected; tests use owned local TLS servers.
- Focused race tests/vet/build passed for canonical reads, changed networks,
  missing code, untrusted certificates, provider errors, receipt absence/reorg,
  redirects, oversized responses, and caller deadlines. All four Actions passed.
- Next: record verified preparation and remaining real-pilot blockers. Selected
  custody and an authorized immutable-byte submission/retry adapter, Base total
  fee policy, deployment inputs/approval, and real-host acceptance are outstanding.


## Phase 2 handoff and next task

Verified and pushed preparation milestones:

| Commit | Result |
| --- | --- |
| `0a0c8d4` | Private online metadata backup and restore validation |
| `d3df72a` | Authenticated operational capacity/readiness reports |
| `4560107` | Separate-machine templates, TLS, distinct node credentials, operator guide |
| `a50aa6b` | Encrypted process-loss, repair, metadata restart/restore, and outage drill |
| `70a31cd` | Guarded Base Sepolia script, unsigned plan, and read-only preflight |
| `8023ab3` | Offline signed-intent journal, nonce fencing, and confirmation policy tests |
| `4199d14` | Read-only HTTPS testnet observations and canonical pinned readback |

All four Actions passed for the last implementation commit:
[Go](https://github.com/web3wizzz/tank/actions/runs/38052110826),
[SDK](https://github.com/web3wizzz/tank/actions/runs/38052110812),
[contracts](https://github.com/web3wizzz/tank/actions/runs/38052110825), and
[browser pilot](https://github.com/web3wizzz/tank/actions/runs/38052111115).
Local validation includes full Go race/vet/build, focused security regressions,
19 frontend tests/lint/build, 15 SDK tests in CI, 11 contract tests including 256
fuzz runs, launcher/template/preparation checks, and schema7 pilot restore.

The agreed local preparation tasks are complete. Real pilot completion is not
claimed. The user confirmed that actual hosts, RPC endpoint, and signing inputs
are not selected and forbids remote provisioning or testnet broadcast for now.
No real RPC/wallet/host was invented; no testnet transaction, remote deployment,
paid infrastructure, or change to existing local storage was performed.

Remaining gates, before a real pilot:

1. Select independent machines, stable HTTPS/DNS endpoints, operator custody,
   RPC provider, signing method, pilot users, and retention/funding/exit terms.
2. Integrate the selected signer and an explicitly authorized submission/retry
   adapter with the prepared journal/reader. Include Base L1/security fees and
   approved total-spend/balance policy; no public broadcaster is enabled now.
3. Review a concrete deployment/simulation plan and obtain deployment/broadcast
   authorization, then execute and verify the selected testnet deployment.
4. Record actual independent-machine failure, repaired encrypted recovery, and
   metadata restore evidence. Loopback process tests cannot establish this.

Next task when the required choices are available: signer/custody integration and
review of the real deployment configuration. Do not proceed into payments,
collateral, larger/resumable transfers, rollup DA, or unrelated landing features.
Preserve private configuration, existing data, logs, backups, and generated
artifacts; all remain ignored/untracked. Signed journals are private operational
state even though they contain no recovery or signing key.
