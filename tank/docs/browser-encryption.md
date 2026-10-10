# Browser file encryption

New files tanked from the workspace are encrypted with Web Crypto AES-256-GCM before submission. Each file gets a fresh random 32-byte key and 12-byte IV. The original filename is inside the encrypted payload. Storage receives a SHA-256 ciphertext ID and an ID-based .tankenc filename.

## User workflow

1. Sign in, select a file, and choose Encrypt file.
2. Download the .tank-key.json recovery file and confirm you saved it privately.
3. Choose Tank encrypted file.
4. Later, choose the recovery file in the retrieval panel; this selects its file ID. Retrieve the file to verify and decrypt it in the browser.

The recovery file contains the unencrypted secret key. It is never submitted to the API and is not stored in browser localStorage. Keep it in a private, backed-up location. Losing it prevents recovery. Someone with both the encrypted data and key can decrypt it; signing out does not invalidate a downloaded recovery key. Browser memory clearance is best effort, not guaranteed. The application cannot confirm the recovery file was actually saved.

Older files and direct CLI/SDK tanking remain unencrypted. This feature does not migrate them. To encrypt an older file, retrieve it and tank it again using the browser flow. Backend authentication and file ownership remain required.

## Format v1

Envelope: 8-byte TANKENC/NUL magic, 1-byte version, 12-byte IV, then AES-GCM ciphertext with a 16-byte authentication tag. The 21-byte header is authenticated as additional data. Plaintext: a big-endian uint32 metadata length, UTF-8 JSON containing the filename, then original file bytes. Metadata is bounded to 4096 bytes. Filename validation rejects paths, controls, bidi controls, and names over 255 UTF-8 bytes.

A recovery JSON contains version=1, algorithm=AES-256-GCM, file_id=SHA256(envelope), and key=32 random bytes encoded as lowercase hex. The browser verifies the ciphertext hash before decrypting and fails closed on GCM authentication errors. The API does not need an encryption migration because it stores opaque bytes.

The plaintext limit is 16 MiB minus 4137 bytes, reserving room inside the existing storage limit. File length, access patterns, ciphertext IDs, and account relationships remain observable. Independent encryptions of identical files have different IDs and are not deduplicated. Browser compromise or malicious application JavaScript can expose keys and plaintext. This MVP implementation has automated tests but has not had an independent security audit.

## Checks

From frontend/:

```bash
node --experimental-strip-types --test test/file-crypto.test.mjs
npm run lint
npm run build
```

Never commit recovery files; *.tank-key.json is ignored at the repository root.


## Browser end-to-end check

The local launchers persist an exact `TANK_FRONTEND_ORIGIN` in `.env.tank-local` for Codespaces,
using `CODESPACE_NAME`, `PORT` (default 3000), and
`GITHUB_CODESPACES_PORT_FORWARDING_DOMAIN` (default app.github.dev). An existing
`TANK_FRONTEND_ORIGIN` is preserved. Edit the value in `.env.tank-local` to change it. Outside Codespaces, set it to the
browser's HTTP(S) origin when using a reverse proxy. Restart the frontend after
changing the origin. The setting is server-only; no wildcard origins are allowed. Codespaces rewrites
its matching HTTPS `Origin` to a localhost backend origin. The workspace sends
`X-Tank-Origin` with the exact browser origin. The server accepts that rewrite
only for a loopback HTTP origin matching the backend Host, an exact forwarded
host, forwarded HTTPS, and a `same-origin` fetch marker. Missing or spoofed
values fail closed.

With the existing Codespaces `GITHUB_TOKEN` available, the test can authenticate
the real private HTTPS port without changing its visibility or logging the token:

```bash
TANK_E2E_CREDENTIAL_FILE=/absolute/path/to/private-user-credential.json \
TANK_E2E_OTHER_CREDENTIAL_FILE=/absolute/path/to/another-private-user-credential.json \
TANK_E2E_CODESPACES_AUTH=1 npm run test:browser
```

This mode uses the actual browser network stack and the actual Codespaces
forwarding transport. It handles the gateway's Continue page only for the current
codespace's exact forwarded URL. The second credential enables a cross-user file
isolation check. Existing users are never revoked by this command.

With local storage services and the production frontend running, from `frontend/`:

```bash
npx playwright install --with-deps chromium
TANK_E2E_CREDENTIAL_FILE=/absolute/path/to/private-user-credential.json npm run test:browser
```

The credential JSON must contain an individual user's `token`. The test uses the
Codespaces HTTPS origin by default, or `http://localhost:3000` outside Codespaces.
Set `TANK_FRONTEND_ORIGIN` in the test environment to target another URL.

When the forwarded URL requires a GitHub login unavailable to the automated
browser, test the origin handling through a local proxy simulation:

```bash
TANK_E2E_CREDENTIAL_FILE=/absolute/path/to/private-user-credential.json \
TANK_E2E_UPSTREAM_URL=http://127.0.0.1:3000 npm run test:browser
```

This runs Chromium at the real Codespaces HTTPS origin, forwarding requests to
the local production frontend while reproducing the localhost Host and Origin
rewrite and preserving the external forwarded Host. It verifies
origin matching, secure HttpOnly sessions, browser Web Crypto, encrypted PDF
upload, reload, missing/wrong key rejection, original Unicode download filename,
byte-for-byte recovery, and logout against live storage. It does not test the
GitHub authentication gateway or the actual Codespaces forwarding transport.

The test creates a small encrypted PDF in the user's storage. Credentials and
recovery keys are never logged. Recovery downloads are read from Playwright's
temporary storage and removed when the browser context closes; the test does not
retain a recovery key for the stored test artifact.
