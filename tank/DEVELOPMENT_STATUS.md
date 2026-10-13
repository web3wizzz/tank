# Tank Phase 1 development status

## Current continuation scope — 2026-10-13

Continue the hosted Arc Testnet pilot backlog in [`AGENTS.md`](AGENTS.md). The
public Arc Testnet deployment and registration are complete at the end of this
file; do not redeploy. Frontend redesign and workspace verification are published
in `78e27a8`. Independent hosted application/storage acceptance remains unproven
because hosts and operator access have not been selected. Local process drills
must not be reported as host evidence.

Frontend verification is complete. This continuation audits reproducible builds,
service lifecycle, TLS, per-node credentials, persistence, bounded retries, and
restore safety; it adds preflight automation and operator materials. Next, resolve
the remote drill interface gap and select independent hosts before hosted
acceptance. Monitoring/backup measurement and capacity evidence must be completed
on selected hosts. Retention, funding, and operator-exit terms remain drafts for
owner review.
Mainnet, real-fund spending, paid hosting, force pushes, data deletion, and budget
increases remain unauthorized. Preserve the deployed registry, signer, journals,
confirmed jobs, and private data. The frontend implementation used the previously
user-approved `gpt-5.6-sol` agent because exact `gpt-5` is not selectable here.

### Hosted pilot operations readiness — in progress (2026-10-13)

- Frontend follow-up commit `78e27a8` improved text contrast and removed the
  stale stock favicon. Responsive checks at 320, 375, 768, 1280 and 1440 px,
  reduced-motion visibility, mobile-menu Escape/focus handling, and custom icon
  and social-preview responses passed. The full isolated browser pilot passed,
  including encryption, recovery-key export/import, exact bytes and Unicode
  filename, registration UI, cross-user denial, pagination, node loss/repair,
  restart, backup/restore, and credential/session behavior.
- One local synthetic 591-byte PDF sample recorded 290.11 ms encryption,
  126.36 ms upload/acknowledgement, and 67.74 ms retrieve/verify/decrypt. It is
  loopback-only evidence, not hosted capacity or latency data. Details and limits
  are in [pilot operations](docs/pilot-operations.md).
- Operations templates now add a loopback frontend unit and disabled bounded Arc
  worker timer, correct worker limits to the authorized 65 Gwei and 0.008-per-file
  ceilings (deployment 0.04 and aggregate 0.10 unchanged), and configure only the
  already verified public testnet registry/address. Backup guidance preserves and
  reconciles newer signed transaction journals before any restored worker resumes.
- Added a configurable hosted preflight runner and 29 unit tests, plus onboarding
  operations, troubleshooting, unreleased notes, and owner-decision drafts. The
  runner checks distinct host identities, HTTPS/TLS endpoints, public Arc chain
  and registry identity, service lifecycle, and private config metadata without
  reading secrets. GPT-6-Sol review fixed a duplicate-host identity gap and added
  coverage for duplicate HTTPS origins and malformed host configuration. Its
  destructive hosted drill deliberately fails closed: the
  current browser/restore interfaces cannot safely coordinate remote node loss
  and signed-journal reconciliation. No independent host inventory or credentials
  are configured, so hosted acceptance remains a blocker.
- Local checks: pilot template tests (4), hosted-runner tests (29), Python compile,
  frontend 19 tests/lint/build and full local pilot passed. CI for `78e27a8` passed
  all four workflows: [Go](https://github.com/web3wizzz/tank/actions/runs/38484967120),
  [SDK](https://github.com/web3wizzz/tank/actions/runs/38484967105),
  [browser](https://github.com/web3wizzz/tank/actions/runs/38484967115), and
  [contracts/Arc-local](https://github.com/web3wizzz/tank/actions/runs/38484967109).
- Next: publish this local operations milestone, then implement/validate safe
  remote drill hooks or document their precise interface requirements. Select
  independent hosts and operator access before attempting real hosted acceptance;
  prepare no paid resources without approval.

Status: TankRegistry is deployed and verified on Arc Testnet. Public-chain signed
registration, canonical commitment readback, encrypted retrieval and user isolation
are verified with an isolated local application/storage stack. Independent hosted
application/storage pilot acceptance and retention terms remain outstanding.

Scope: complete the local MVP backlog below. Verified milestones may be committed
and pushed to the existing origin. The latest user authorization permits Arc
TESTNET deployment/registration with testnet funds only after prerequisites pass,
superseding historical broadcast restrictions below. Mainnet, paid infrastructure,
real-fund spending, force pushes, secret exposure and existing-data deletion remain
unauthorized. Earlier milestone sections retain their historical authorization.

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

The earlier Base Sepolia preparation is superseded by the Arc Testnet work below.
The agreed earlier local preparation tasks are complete. Real pilot completion is not
claimed. The user confirmed that actual hosts, RPC endpoint, and signing inputs
are not selected and forbids remote provisioning or testnet broadcast for now.
No real RPC/wallet/host was invented; no testnet transaction, remote deployment,
paid infrastructure, or change to existing local storage was performed.

Remaining gates, before a real pilot:

1. Select independent machines, stable HTTPS/DNS endpoints, operator custody,
   RPC provider, signing method, pilot users, and retention/funding/exit terms.
2. Integrate the selected signer and an explicitly authorized submission/retry
   adapter with the prepared journal/reader. Include Arc native USDC gas accounting and
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


### Arc milestone 1: explicit testnet roadmap and bounded chain policy

- Arc Testnet replaces Base Sepolia as the first public target. Official network,
  EVM, gas and tooling docs verified 2026-10-11; current endpoints use arc.io.
- Deployment script, unsigned plan, templates and CI preparation checks now bind
  chain 5042002. No public RPC default, wallet selection or broadcast is enabled.
- Signed intent policy requires explicit Arc/local chain identity and enforces
  Arc's 20 Gwei minimum fee cap. Journal targets retain chain/registry/registrant
  namespaces; account nonce lanes remain exclusive across registry contracts.
- Registry and guarded deployment: all 11 tests pass in standard Foundry and Arc
  Foundry v0.8.0-2 with 256 fuzz cases. Python preparation checks pass.
- Actual network deployment remains blocked on securely supplied selected RPC,
  dedicated signer and testnet USDC funds, followed by separate authorization.
  Arc registry deployment does not host frontend/coordinator/storage nodes.
- Next independent preparation: secure custody adapter, signed worker recovery
  and Arc-Anvil integration evidence. Historical Base milestones above are history.


### Arc milestone 2: offline custody and recoverable signed worker — `620fc1d`

- Added encrypted V3 keystore signing with private owned-file checks, symlink
  refusal, address binding, redacted errors, and Arc type-2/zero-value restrictions.
- Added a manually invoked preparation command with private journal-directory and
  sidecar checks; no broadcaster or automatic service activation is present.
- Signed worker resumes journaled bytes across retries/restart. Chain, registry,
  and registrant namespace jobs; chain/account lanes exclude concurrent nonces.
- Read-only terms check current native USDC fees, gas margin, balance and explicit
  spend caps before signing. Caps are never increased automatically.
- Validation: full Go race suite, vet/build, custody and reader regression tests,
  retry/lane tests and private-journal checks passed. Public signing inputs remain
  pending; tests use generated isolated accounts only.
- Remaining public gate: securely supplied RPC, dedicated signer and testnet USDC,
  aggregate spend/submission policy and separate broadcast authorization.


### Arc milestone 3: reproducible Arc execution and CI acceptance — `9895780`

- Reverified official connection, EVM and gas documentation on 2026-10-12. Current
  testnet chain 5042002 and arc.io endpoints remain explicit reference parameters,
  never an automatic operational RPC selection. Mainnet remains excluded.
- Added official Arc Foundry v0.8.0-2 installation pinned by archive SHA-256 and
  commit, with an Arc-only loopback integration script and Contract CI job.
- Local Arc tests deploy TankRegistry with a generated signer, prepare and submit
  signed registration only to owned nodes, verify receipt-native USDC gas costs,
  retry absent receipts/exact bytes, and reopen SQLite before canonical completion
  without signing again. Transaction types0/1/2/4 execute successfully locally.
- Forbidden nonzero zero-address transfer fails on execution. Arc Anvil's estimate
  alone did not reject it; successful receipts/readback remain mandatory. Native
  precision is 18 decimals, following official tables/EVM docs; their conflicting
  6-decimal native-send example is explicitly documented and not copied.
- Validation passed: 11 standard and 11 Arc contract tests with 256 fuzz runs,
  both Arc Go integration tests with race detection, full Go race suite, vet/build,
  module verification, shell syntax, and Python launcher/template/preparation
  tests. README, contributor guide, examples and worker docs reflect these gates.
- Custody milestone620fc1d pushed to origin/main. Public-network/provider acceptance
  remains untested while selected RPC, dedicated signer and USDC funds are pending.
  No public transaction, paid provisioning, or mainnet deployment was performed.
- Registry deployment and frontend/coordinator/independent storage hosting retain
  separate acceptance requirements. Future submission integration and aggregate
  spend policy remain prerequisites to any authorized public deployment.


### Arc preparation handoff: publication and CI verified

Both `620fc1d` (secure offline custody/worker) and `9895780` (Arc local tooling/CI)
are pushed to origin/main. All four workflows passed for both commits. Final
implementation evidence:

- [Go race/static/build checks](https://github.com/web3wizzz/tank/actions/runs/38289281695)
- [Standard contracts and Arc local execution/recovery](https://github.com/web3wizzz/tank/actions/runs/38289281700)
- [TypeScript SDK](https://github.com/web3wizzz/tank/actions/runs/38289281696)
- [Browser workspace and pilot recovery](https://github.com/web3wizzz/tank/actions/runs/38289281681)

Independent preparation is verified; actual Arc Testnet acceptance is not claimed.
Continue only after the chosen RPC, dedicated signing account and testnet USDC
funds arrive through secure configuration, with a reviewed submission/spend policy
and explicit broadcast authorization. No secrets should be supplied in chat.
Mainnet, paid infrastructure and public broadcasts remain outside authorization.
Frontend/coordinator/independent storage hosting remains a separate pilot gate.


### Arc deployment milestone: guarded execution ready; public prerequisites blocked

Latest user authorization permits TankRegistry deployment and registration on
Arc TESTNET only after network/custody/funding/fee prerequisites pass. It replaces
the earlier restriction on testnet broadcasting. No new approval is needed for
that authorized execution. Mainnet and real-fund/paid-host spending remain excluded.

- Rechecked official connection/gas docs on 2026-10-12: testnet5042002,
  mainnet5042, native USDC gas with 18 decimals. Read-only chain-ID checks against
  official `rpc.testnet.arc.io` and documented `rpc.drpc.testnet.arc.io` returned
  HTTP403 even with sandbox network escalation. Connectivity/actual RPC chain
  verification has therefore NOT passed; no working provider was invented.
- Active environment lacks selected RPC, expected dedicated public sender,
  encrypted keystore/password paths, metadata journal and explicit fee budgets.
  Existing local data was preserved. Custody validity, testnet balance and public
  gas estimates cannot be checked until those inputs are configured privately.
- Added `tank-deploy-arc preflight|deploy`: explicit testnet/HTTPS chain checks,
  private encrypted custody, current gas/fee estimates and balance, operator
  ceilings, zero-value type2 creation, exclusive durable no-overwrite signed-byte
  journal before submission, immutable nonce/hash retries and restart recovery.
  Successful completion requires canonical receipt plus exact compiled runtime
  equality and block/network rechecks. Output contains public identifiers only.
- Added opt-in `tank-prepare-registration --broadcast` with compiled runtime
  verification. Preparation remains the default. Submission uses validated
  journaled bytes and rechecks the live chain before send; ambiguous provider
  errors are redacted and preserve the intent. The existing unlocked Anvil worker
  retains its loopback-only restriction.
- Coordinator opt-in `arc-testnet-external` mode reports the separate worker's
  namespaced status from the shared database without RPC or signing custody;
  authenticated status and cross-user denial regressions passed.
- Additive schema8 stores permanent conservative maximum-cost reservations and
  an immutable aggregate registration budget per account. Atomic concurrent
  reservations, idempotent retry and restart persistence are verified. Deployment
  has a separate cap. One dedicated account/journal must own its nonce/spend lane;
  no fee replacement, budget escalation or journal deletion is automatic.
- Explicit mainnet regressions cover Arc5042, Ethereum1, local31337, Base84532,
  malformed/unexpected networks, signing chain and changed RPC chain, preventing
  custody/submission on those networks. Solidity deployment regression also
  rejects Arc and Ethereum mainnet with a valid configured sender.
- Local Arc acceptance verifies the production guarded deployer, exact runtime,
  restart with closed signer and zero remaining balance, and unchanged bytes/hash/
  address/nonce. Production broadcaster acceptance uses generated encrypted
  custody through trusted test TLS on an owned Arc node, actual registration,
  ambiguous response, SQLite reopen without signing/submitting again, canonical
  commitment/size readback and persisted aggregate-budget rejection.
- Full Go race suite, vet and command builds passed. Arc contract suite passed
  12 tests including 256 fuzz cases; all four Arc integration tests passed with
  race detection. Launcher5/template2/preparation3 checks and diff checks passed.
  Local encrypted browser pilot passed: ciphertext upload, exact bytes/Unicode
  filename after local decryption, cross-user denial, process loss/repair,
  metadata backup/restore, restart recovery, outage threshold and credentials.
  These are isolated local tests, not public testnet or remote-signer evidence.
- Exact terminal-only key/account, password, faucet, budget, deployment and worker
  commands are in [Arc deployment setup](docs/arc-testnet-deployment.md). No keys,
  passwords, credentials, raw journals or recovery keys were printed or committed.

Public acceptance report: **no deployed address; no deployment/registration
transaction hashes; no public explorer transaction yet**. Required next inputs:
working selected HTTPS testnet RPC; securely configured dedicated signing custody
and public address; faucet testnet USDC; explicit fee budgets and private journal.
The available custody adapter signs from a local encrypted keystore on the worker
host. A remote signing service has not been selected/integrated. Public registration,
on-chain readback, retry/restart and combined encrypted-file acceptance remain
outstanding until those prerequisites pass.

Remaining hosting requirements: selected frontend/coordinator HTTPS hosts, three
independent active storage machines plus spare, per-node private credentials,
capacity/monitoring, private backups and restore drills, retention/funding/operator
exit terms and real independent-machine failure acceptance. Registry deployment
stores no shards and does not complete application hosting. No paid provisioning
or mainnet execution occurred.


### Arc milestone: public deployment, signed registration and encrypted-file acceptance

- User approved 65 Gwei per-gas ceilings for Arc TESTNET only; deployment 0.04,
  per-registration 0.008 and aggregate0.10 testnet USDC ceilings remain unchanged.
- Secure dedicated encrypted custody and faucet funding passed. Official public
  Blockdaemon RPC passed chain 5042002, verified TLS and canonical hash-pinned reads.
  Sandbox DNS restrictions and HTTP403 from other documented RPCs were diagnosed
  separately. No secret RPC URL, password or key was printed/transferred/committed.
- TankRegistry: `0xe1fC5025261af42dfe04788c7a09DAd3ea1EC435`.
  Deployment transaction:
  `0x21530cecade1981109209048f67e3e72ee17d4a45dd39c65c6a031ad5072089f`.
  Canonical receipt and exact compiled runtime verified; actual fee 0.004998504
  testnet USDC. Resuming the existing private journal returned the same address/hash
  and left its bytes unchanged. No duplicate deployment or competing nonce created.
- Registration transaction:
  `0xfc61320016d6fb4d54773a75f75cb773ad8998dfee60a484947dc72b796a0fab`.
  Actual fee 0.001457379 testnet USDC; permanent maximum-cost reservation 0.00548002,
  within the unchanged budgets. A nonbroadcast preparation pass, delayed retry in
  a fresh worker and another restart verified immutable bytes/signature/nonce and
  canonical commitment/size confirmation. The private lane remains nonce 1 confirmed.
- Encrypted synthetic file passed real public-chain registration plus isolated
  local application/storage upload/retrieval, exact bytes/Unicode filename after
  local decryption, cross-user 404, and retrieval after coordinator/storage restart.
  Independent receipt-block RPC reads recomputed/matched commitment and 2133-byte
  ciphertext size. Existing local user data was preserved; pilot data stays private.
- Arc Studio contract-only review began with both clones at main/67bc932 full SHA,
  and both target files matched independently by content/hash. Zero-sender rejection
  is already correct and its test passes in both environments; no test weakening
  or artificial code patch was applied. Arc Studio corrected an unsupported initial
  tooling hypothesis and returned no source changes, with 12 passing tests/256 fuzz
  cases. Codex independently passed 12 Arc contract tests. Arc Studio had no custody
  or journal input and performed no deployment, broadcasting, commit or push.
- Post-review preflight passed within 65 Gwei and unchanged total limits. Existing
  deployment and confirmed registration were reverified, and encrypted retrieval/
  decryption/user isolation repeated without additional transactions.
- [Acceptance report](docs/arc-testnet-acceptance.md) and
  [public identifiers/bytecode/fee evidence](deploy/arc-testnet/registry.json)
  document results and explorer links. Setup commands now securely derive the
  public address even when a Cast V3 keystore omits its optional address field.

Public blockchain milestone is complete. Application/storage above ran locally;
independent hosted pilot acceptance, stable HTTPS, retention/funding/operator-exit
terms and real machine-failure acceptance remain outstanding. The selected custody
is a local encrypted keystore, not a remote signing-service adapter. Pilot services
are stopped with private data/configuration retained. No mainnet deployment, real
fund spending or paid provisioning occurred. Historical blocker sections above
retain the state at their earlier milestones; this section records current gates.
