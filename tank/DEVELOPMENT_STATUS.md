# Tank Phase 1 development status

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

## Remaining backlog, in order

1. Improve frontend accessibility, responsiveness, and error handling.
2. Update SDK documentation, README, CONTRIBUTING.md, and SECURITY.md.
3. Strengthen CI for Go, contracts, SDKs, and frontend.
4. Review the complete local MVP and fix reproducible defects.

## Blockers and working-tree notes

- No essential blocker currently known. The existing main branch is unprotected,
  origin/main matches the local HEAD, and GitHub API authentication works.
- Preserve pre-existing unstaged README/CONTRIBUTING and CI changes. Stage only
  milestone changes. Local environment, credentials, keys, storage, databases,
  logs, dependencies, and build output must remain ignored and untracked.
- Live storage and Anvil services belong to the user. Isolated integration tests
  create and stop their own temporary stack; never stop the user's storage nodes.

## Next task

Finish and publish verified resource bounds and quota accounting, inspect Actions,
then fix the reviewed frontend accessibility/responsiveness/error-handling issues.

## Completed milestone: frontend usability

- Fixed 320px navigation overflow, repeated file selection, stale encryption FAQ,
  robust non-JSON sign-in errors, focus after sign-in/logout/expiry, and upload
  guidance using the configured encrypted-file size bound.
- Frontend 19 tests, lint, production build, and isolated integration passed.
  Browser checks cover 320/375/768px signed-in/out layouts, keyboard submission,
  HTML gateway failures, repeat selection, and focus restoration.
- Publication/Actions pending. Next: SDK/security/contributor documentation.
