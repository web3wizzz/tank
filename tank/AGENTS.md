# Tank operating scope

Continue toward a reliable hosted Arc Testnet pilot. Arc Testnet chain 5042002
registry deployment and signed registration are complete and documented in
`docs/arc-testnet-acceptance.md`; verify and reuse that evidence. Never redeploy
to repeat completed checks.

## Boundaries

- Preserve the deployed registry, signing account, transaction journals,
  confirmed jobs, private data, recovery keys, and approved budgets.
- Use the existing Arc Testnet target and testnet funds only. Mainnet deployment,
  real-fund spending, paid infrastructure, force pushes, and deleting user data
  are unauthorized.
- Never print, prompt for, transfer, or commit credentials, keystores, passwords,
  private databases, signed journals, or recovery keys. Use private local paths.
- Record missing hosts, credentials, access, and owner decisions as blockers;
  continue work that does not need them. Never claim a local simulation proves
  independent-host availability.
- Give agents exclusive file ownership. The primary agent owns chain broadcasts,
  transaction journals, integration, commits, and pushes.

## Ordered backlog

1. Complete and verify the frontend redesign and the real storage workspace.
2. Audit hosted readiness: reproducible builds, service lifecycle, TLS,
   per-node credentials, persistent volumes, monitoring, bounded retries, and
   backup/restore safety.
3. Add configurable hosted acceptance automation. Cover encrypted tanking,
   Arc registration/readback, retrieval, node loss, spare repair, restart,
   recovery, and user isolation. Label simulated and independent-host evidence.
4. Close actionable operational gaps with focused tests.
5. Prepare onboarding, operator runbooks, troubleshooting, release notes, and
   measured capacity/performance evidence. Draft retention, funding, and exit
   terms for owner review; do not claim approval or permanent retention.
6. Run final release-readiness review and fix actionable defects.

For each milestone: implement, review, run relevant checks, inspect staged files
for private artifacts, commit, push, check CI, and update
`DEVELOPMENT_STATUS.md` with evidence and remaining blockers.

Current local restore work uses a persistent signing gate and shared process lock,
strict newest-live/snapshot equality (including revocations and spend history), and
read-only canonical chain reconciliation. Never bypass a failed gate or activate
its restore candidate. Remote execution is disabled pending reviewed activation,
browser coordination, and selected hosts/operator access. Continue safe local
interface work; do not repeat unchanged passing drills or public deployment.
