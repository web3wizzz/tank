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

## Current milestone: authentication and account continuity

- Replacement credentials preserve user identity, file permissions, and original
  filenames. Existing credentials remain valid until expiration or explicit
  revocation. Administrative metadata listings expose no token or token hash.
- Issuance refuses existing destinations and symlinks; failed saves preserve
  previous credentials and clean up only newly created credential artifacts.
- Metadata and CLI regression tests pass. Browser integration verifies a revoked
  credential cannot sign in, then a replacement credential retrieves the user's
  existing encrypted PDF with the original recovery key.
- Browser file-list pagination is implemented; its 101-file integration check is
  in progress. Keep pagination changes separate from the credential commit.
- Publication and GitHub Actions verification are pending.

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

Finish focused account-continuity/pagination commits, push each, inspect Actions,
and fix failures before beginning resource limits and quotas.
