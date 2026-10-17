# Opt-in encryption for the CLI and SDKs

The CLI and SDK raw-byte upload/retrieval APIs remain unchanged. Opt in to local
AES-256-GCM encryption with the helpers below. The browser workspace, Go SDK,
TypeScript SDK and CLI use the same version 1 envelope and recovery JSON. No key
or original filename is sent to the coordinator by these examples.

| Interface | Encryption behavior |
| --- | --- |
| Browser workspace | Encrypts locally before upload; recovery export/import is required |
| CLI `tank tank` | Raw by default; `--encrypt --recovery-out` opts in |
| CLI `tank retrieve` | Stored bytes by default; `--recovery` verifies and decrypts |
| Go/TypeScript raw client APIs | Preserve raw bytes; call encryption helpers explicitly |
| Coordinator/storage nodes | Store ciphertext when supplied; do not possess recovery keys |

File IDs remain SHA-256 hashes of the **stored bytes**. Encrypted IDs hash the
complete ciphertext envelope; fresh keys and nonces make repeated encrypted
uploads of the same plaintext produce different IDs. Registration concerns that
ciphertext ID, and successful storage does not imply completed registration.

Recovery JSON is a secret, separate from the private user API token. Store it
privately before submitting the encrypted bytes. Never log it, place it in a
repository, pass it as a command-line value, or send it to Tank. The key alone
cannot authorize retrieval, and a token cannot decrypt. Lost keys cannot be
recovered by Tank. Keep an independent private backup of the recovery file.
JavaScript strings and Go strings containing recovery keys cannot be reliably
zeroed; the helpers clear temporary byte buffers, not caller-owned plaintext or
returned secret records. Keep recovery records in memory only as long as needed.

## CLI

Supply the existing user token through `TANK_API_TOKEN` in your private process
environment. Keep the recovery path on a private local filesystem.

```bash
tank tank report.pdf --encrypt --recovery-out /private/report.recovery.json
tank retrieve CIPHERTEXT_FILE_ID --out restored.pdf --recovery /private/report.recovery.json
```

Encrypted upload prints only the ciphertext ID. `--encrypt` requires
`--recovery-out`, and `--recovery-out` requires `--encrypt`. Recovery output is
created with mode 0600, synced before uploading, and never overwrites an existing
path. If upload fails or its result is uncertain, the recovery file remains;
retain it for that attempt. A new attempt needs a new recovery path and creates
fresh ciphertext. Inspect the coordinator's authorized list/status before
assuming a failed request did not store the first attempt.

Retrieval without `--recovery` returns verified stored bytes as before. With it,
the CLI imports bounded recovery JSON, checks its file ID, verifies the complete
ciphertext hash, authenticates GCM and metadata, then creates the requested
`--out` file privately without overwriting it. The encrypted original filename
is validated but does not choose or replace your output path.

## Go SDK

`EncryptFile(data, filename)` returns ciphertext and a `Recovery` record;
`ExportRecovery(record)` and `ParseRecovery(jsonBytes)` export/import its JSON.
`DecryptFile(stored, record)` returns authenticated plaintext and the original
filename. These helpers are local and make no network calls.

```go
stored, recovery, err := tank.EncryptFile(data, "report.pdf")
if err != nil { return err }
secret, err := tank.ExportRecovery(recovery)
if err != nil { return err }
defer clear(secret)
f, err := os.OpenFile(recoveryPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
if err != nil { return err }
if _, err = f.Write(secret); err != nil { f.Close(); return err }
if err = f.Sync(); err != nil { f.Close(); return err }
if err = f.Close(); err != nil { return err }
manifest, err := client.Tank(ctx, stored)
if err != nil { return err } // Keep the private recovery file.
_ = manifest
```

Import from a private file using a bounded read of at most 2049 bytes, then:

```go
recovery, err := tank.ParseRecovery(secret)
if err != nil { return err }
stored, err := client.Retrieve(ctx, recovery.FileID)
if err != nil { return err }
plain, filename, err := tank.DecryptFile(stored, recovery)
if err != nil { return err }
// Save plain privately using your application-chosen path; do not trust a path from metadata.
_ = plain
_ = filename
```

## TypeScript SDK (Node 24+)

`encryptFile(arrayBuffer, filename)` and `decryptFile(arrayBuffer, recovery)`
operate on ArrayBuffers. For a Buffer or Uint8Array, use
`Uint8Array.from(bytes).buffer` to copy exactly its view, including slices.
`parseRecovery(text)` imports recovery JSON and `exportRecovery(record)` exports
validated JSON. All are exported from the SDK entry point.

```js
import { Tank, encryptFile, decryptFile, parseRecovery, exportRecovery } from "./sdk-ts/dist/index.js";
import { open } from "node:fs/promises";

const client = new Tank({ baseURL, token: process.env.TANK_USER_TOKEN });
const encrypted = await encryptFile(Uint8Array.from(data).buffer, "report.pdf");
const handle = await open(recoveryPath, "wx", 0o600);
try {
  await handle.writeFile(exportRecovery(encrypted.recovery), "utf8");
  await handle.sync();
} finally { await handle.close(); }
const manifest = await client.tank(new Uint8Array(encrypted.bytes));
// Keep the recovery file even if an upload fails or its result is uncertain.
```

Read at most 2049 bytes from a private recovery file before passing its UTF-8 text
to `parseRecovery`; it rejects records larger than 2048 characters. Then:

```js
const recovery = parseRecovery(recoveryText);
const stored = await client.retrieve(recovery.file_id);
const restored = await decryptFile(Uint8Array.from(stored).buffer, recovery);
// restored.bytes is plaintext; restored.filename is validated metadata.
```

Do not pass `{ filename: originalName }` to `client.tank` for an encrypted file:
that option sends plaintext metadata. The original filename already travels
inside the authenticated encrypted payload.

## Format and limits

The envelope is `TANKENC\0` (8 bytes), version `1` (1 byte), a random 12-byte GCM
nonce, and AES-256-GCM ciphertext with a 16-byte tag. The full 21-byte header is
GCM additional authenticated data. The encrypted payload is a big-endian
4-byte metadata length, UTF-8 JSON `{ "filename": "…" }`, and nonempty original
bytes. Recovery JSON is `{ "version": 1, "algorithm": "AES-256-GCM",
"file_id": "64 lowercase hex characters", "key": "64 lowercase hex characters" }`.

Maximum stored size is 16 MiB; maximum plaintext is 16 MiB minus 4137 bytes
(`MaxPlainBytes` / `MAX_PLAIN_BYTES`), reserving 4096 bytes of metadata. Names
must be valid Unicode basenames of 1–255 UTF-8 bytes without paths, control
characters or bidirectional control characters. Imports reject unsupported
versions, wrong IDs, wrong keys, tampering and invalid metadata before returning
plaintext. Recovery file paths and output paths are always caller-selected.

## Compatibility verification

Run `go test ./pkg/tank ./cmd/tank` with Go 1.26 and Node 24, plus `npm test
--prefix sdk-ts`. The Go compatibility test uses the actual browser and SDK
sources, proving Go-to-browser, Go-to-TypeScript, browser-to-Go,
TypeScript-to-Go and browser/TypeScript round trips with Unicode filenames and
binary data. If Node is absent that test explicitly skips; a Go-only run is not
the cross-language acceptance gate. All evidence is local; it does not prove
independent-host availability or chain registration.
