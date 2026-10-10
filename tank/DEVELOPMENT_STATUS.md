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

## Current milestone: remaining authentication/session defects

- The independent security review found upload bodies buffered before session
  validation and uncancelled downloads after sign-out. Authenticate before reading
  uploads; abort workspace requests on session end and before plaintext downloads.
- Browser checks passed for headers-only unauthenticated uploads and a held
  authenticated retrieval followed by sign-out: 401 before body upload, and the
  pending request aborts without a late plaintext download.
- Validation: 17 frontend tests, lint, production build, full isolated integration,
  and diff checks passed. Publication and Actions verification are next.
- Read-only reviews also confirmed mobile navigation overflow, stale encryption
  FAQ text, same-file reselection, JSON-only login error handling, focus issues,
  missing contract CI, and incomplete SDK/build workflow coverage. These remain
  assigned to their ordered frontend/docs/CI milestones.

## Remaining backlog, in order

1. Finish authentication/authorization/session fixes and pagination verification.
2. Inspect and add configurable request limits, storage quotas, and concurrency
   bounds, with meaningful failure and isolation tests.
3. Improve frontend accessibility, responsiveness, and error handling.
4. Update SDK documentation, README, CONTRIBUTING.md, and SECURITY.md.
5. Strengthen CI for Go, contracts, SDKs, and frontend.
6. Review the complete local MVP and fix reproducible defects.

## Blockers and working-tree notes

- No essential blocker currently known. The existing main branch is unprotected,
  origin/main matches the local HEAD, and GitHub API authentication works.
- Preserve pre-existing unstaged README/CONTRIBUTING and CI changes. Stage only
  milestone changes. Local environment, credentials, keys, storage, databases,
  logs, dependencies, and build output must remain ignored and untracked.
- Live storage and Anvil services belong to the user. Isolated integration tests
  create and stop their own temporary stack; never stop the user's storage nodes.

## Next task

Publish and verify the session/body-buffering fix, then implement configurable
request limits, storage quotas, and concurrency bounds.
