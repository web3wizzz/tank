# Local MVP verification

The local MVP includes Go storage and repair, local on-chain registration,
Go/TypeScript SDKs, individual credentials, a session-backed browser workspace,
and browser encryption with original filename recovery.

## First-run setup

Use the [repository README](../../README.md#browser-workspace) for setup. Both
local launchers source `scripts/init-local-env.sh`. It generates missing node,
API, and session secrets under a file lock, preserves existing settings, rejects
an environment-file symlink, and limits `.env.tank-local` to mode 0600. It also persists the exact Codespaces
frontend origin when missing, preserving an existing explicit origin. Bash,
OpenSSL, and `flock` (util-linux on Linux) are required.

Keep development environment files and user credential JSON files private.
Credential creation writes a new mode-0600 file and refuses to overwrite an
existing file. Browser sign-in requires an individual user credential, not the
administrator credential.

## Automated checks

From `tank/`:

```bash
bash -n scripts/*.sh
python3 scripts/test-local-env.py
go test -race -count=1 -timeout=5m ./...
go vet ./...
go build ./cmd/...
```

From `tank/sdk-ts/`:

```bash
npm ci
npm test
```

From `tank/frontend/`, after building the linked SDK:

```bash
npm ci
npm test
npm run lint
npm run build
npx playwright install --with-deps chromium
npm run test:integration
```

From `tank/contracts/` with Foundry installed:

```bash
"$HOME/.foundry/bin/forge" fmt --check
"$HOME/.foundry/bin/forge" test
```

## Isolated integration behavior

The browser integration runner builds temporary Go executables, allocates local
ports, starts four storage nodes and a coordinator, creates two private users,
and starts the built Next.js frontend. The browser runs at a configured HTTPS
origin through a local forwarding transport, including the localhost backend
Host and Origin rewrite with an exact external forwarded Host. This exercises the reverse-proxy origin regression without depending on a
GitHub login. The separate `test:browser` mode can also verify the actual private Codespaces
HTTPS URL using the existing gateway token; see the [encryption guide](browser-encryption.md).

The test verifies:

- Exact-origin sign-in, secure HttpOnly sessions, and rejection of missing,
  malformed, unapproved, or cross-site request origins and workspace headers.
- Browser encryption of a valid PDF, with its Unicode filename inside ciphertext.
- A ciphertext content ID and an ID-based storage filename, without sending the
  recovery key in requests.
- A second user cannot list or retrieve the first user's file, even with its ID.
- Retrieval after stopping one owned test storage node.
- Reloaded sessions, missing/wrong recovery key rejection, correct PDF bytes,
  original download filename, and logout.
- Credential revocation rejects an active session's next request and subsequent
  sign-in attempts.

It uses a separate temporary database and storage directories, prints no
credentials or recovery keys, and removes its own processes and files at exit.
It does not stop personal services or modify the existing local-demo database.
Chain registration is disabled in this isolated browser stack; Go registry tests,
Foundry tests, and the existing [registration guide](automatic-registration.md)
cover that subsystem separately.

## Remaining scope

This completes a locally testable MVP, not a production durability guarantee.
Public-chain signing, payments, storage proofs, independent operators, larger
file streaming, user credential rotation, and release publication remain future
work. Browser encryption is not applied automatically to CLI/SDK uploads.
Losing a recovery key prevents browser decryption. Back up private keys and the
original files during evaluation.
