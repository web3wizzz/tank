#!/usr/bin/env bash
set -euo pipefail
cd /workspaces/tank/tank
[[ -f frontend/src/components/workspace.tsx ]] || { echo "Workspace file is missing."; exit 1; }
[[ ! -f frontend/src/lib/file-crypto.ts ]] || { echo "Encryption module already exists. Stop to avoid overwriting it."; exit 1; }
mkdir -p frontend/src/lib frontend/test docs

python3 - <<'PY_IGNORE'
from pathlib import Path
p = Path("/workspaces/tank/.gitignore")
s = p.read_text() if p.exists() else ""
if "*.tank-key.json" not in s.splitlines():
    p.write_text(s.rstrip() + "\n*.tank-key.json\n")
PY_IGNORE

cat > frontend/src/lib/file-crypto.ts <<'TANK_CRYPTO'
export const MAX_STORED_BYTES = 16 * 1024 * 1024;
export const MAX_PLAIN_BYTES = MAX_STORED_BYTES - 4096 - 41;
const MAGIC = new Uint8Array([84, 65, 78, 75, 69, 78, 67, 0]);
const HEADER_BYTES = 21;
const HEX = /^[a-f0-9]{64}$/;
const encoder = new TextEncoder();
const decoder = new TextDecoder("utf-8", { fatal: true });

export interface Recovery {
  version: 1;
  algorithm: "AES-256-GCM";
  file_id: string;
  key: string;
}

function hex(bytes: Uint8Array): string {
  return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
}

export async function fileHash(bytes: ArrayBuffer): Promise<string> {
  return hex(new Uint8Array(await crypto.subtle.digest("SHA-256", bytes)));
}

function safeName(value: unknown): string {
  if (typeof value !== "string" || !value || value === "." || value === ".." ||
      /[\\/\u0000-\u001f\u007f-\u009f\u202a-\u202e\u2066-\u2069]/u.test(value) ||
      encoder.encode(value).byteLength > 255 || decoder.decode(encoder.encode(value)) !== value) {
    throw new Error("Choose a filename without paths or control characters, up to 255 bytes.");
  }
  return value;
}

export function parseRecovery(text: string): Recovery {
  if (text.length > 2048) throw new Error("Recovery file is too large.");
  let value: unknown;
  try { value = JSON.parse(text); } catch { throw new Error("Invalid recovery JSON."); }
  if (!value || typeof value !== "object") throw new Error("Invalid recovery file.");
  const r = value as Record<string, unknown>;
  if (r.version !== 1 || r.algorithm !== "AES-256-GCM" ||
      typeof r.file_id !== "string" || !HEX.test(r.file_id) ||
      typeof r.key !== "string" || !HEX.test(r.key)) {
    throw new Error("Invalid or unsupported recovery file.");
  }
  return { version: 1, algorithm: "AES-256-GCM", file_id: r.file_id, key: r.key };
}

export function isEncrypted(bytes: ArrayBuffer): boolean {
  const data = new Uint8Array(bytes);
  return data.length >= MAGIC.length && MAGIC.every((b, i) => data[i] === b);
}

export async function encryptFile(bytes: ArrayBuffer, filename: string) {
  if (bytes.byteLength < 1 || bytes.byteLength > MAX_PLAIN_BYTES) {
    throw new Error(`Choose a file between 1 and ${MAX_PLAIN_BYTES} bytes.`);
  }
  const metadata = encoder.encode(JSON.stringify({ filename: safeName(filename) }));
  if (metadata.byteLength > 4096) throw new Error("Filename metadata is too large.");
  const plain = new Uint8Array(4 + metadata.length + bytes.byteLength);
  new DataView(plain.buffer).setUint32(0, metadata.length);
  plain.set(metadata, 4);
  plain.set(new Uint8Array(bytes), 4 + metadata.length);
  const header = new Uint8Array(HEADER_BYTES);
  header.set(MAGIC);
  header[8] = 1;
  header.set(crypto.getRandomValues(new Uint8Array(12)), 9);
  const rawKey = crypto.getRandomValues(new Uint8Array(32));
  const key = await crypto.subtle.importKey("raw", rawKey.buffer, "AES-GCM", false, ["encrypt"]);
  let encrypted: ArrayBuffer;
  try {
    encrypted = await crypto.subtle.encrypt({ name: "AES-GCM", iv: header.slice(9).buffer,
      additionalData: header.buffer, tagLength: 128 }, key, plain.buffer);
  } finally { plain.fill(0); }
  const stored = new Uint8Array(HEADER_BYTES + encrypted.byteLength);
  stored.set(header);
  stored.set(new Uint8Array(encrypted), HEADER_BYTES);
  const file_id = await fileHash(stored.buffer);
  const recovery: Recovery = { version: 1, algorithm: "AES-256-GCM", file_id, key: hex(rawKey) };
  rawKey.fill(0);
  return { bytes: stored.buffer, fileID: file_id, recovery };
}

export async function decryptFile(bytes: ArrayBuffer, recovery: Recovery) {
  const record = parseRecovery(JSON.stringify(recovery));
  if (bytes.byteLength > MAX_STORED_BYTES || bytes.byteLength < HEADER_BYTES + 21 ||
      !isEncrypted(bytes) || new Uint8Array(bytes)[8] !== 1) {
    throw new Error("Invalid or unsupported encrypted file.");
  }
  if (await fileHash(bytes) !== record.file_id) throw new Error("Recovery key belongs to a different file, or data is damaged.");
  const rawKey = Uint8Array.from(record.key.match(/../g)!, (b) => parseInt(b, 16));
  const key = await crypto.subtle.importKey("raw", rawKey.buffer, "AES-GCM", false, ["decrypt"]);
  rawKey.fill(0);
  const header = bytes.slice(0, HEADER_BYTES);
  let plain: ArrayBuffer;
  try {
    plain = await crypto.subtle.decrypt({ name: "AES-GCM", iv: header.slice(9),
      additionalData: header, tagLength: 128 }, key, bytes.slice(HEADER_BYTES));
  } catch { throw new Error("Wrong recovery key or damaged encrypted data. Download blocked."); }
  try {
    if (plain.byteLength < 5) throw new Error("Invalid encrypted payload.");
    const size = new DataView(plain).getUint32(0);
    if (size < 1 || size > 4096 || 4 + size >= plain.byteLength) throw new Error("Invalid encrypted metadata.");
    const metadata = JSON.parse(decoder.decode(plain.slice(4, 4 + size)));
    const filename = safeName(metadata?.filename);
    const result = plain.slice(4 + size);
    if (result.byteLength > MAX_PLAIN_BYTES) throw new Error("Decrypted file exceeds the limit.");
    return { bytes: result, filename };
  } finally { new Uint8Array(plain).fill(0); }
}

TANK_CRYPTO

cat > frontend/test/file-crypto.test.mjs <<'TANK_TEST'
import test from "node:test";
import assert from "node:assert/strict";
import { encryptFile, decryptFile, fileHash, parseRecovery, isEncrypted, MAX_PLAIN_BYTES } from "../src/lib/file-crypto.ts";
const input = new TextEncoder().encode("Private PDF bytes — Tank").buffer;

test("binary round trip and encrypted Unicode filename", async () => {
  const result = await encryptFile(input, "Résumé.pdf");
  assert.ok(isEncrypted(result.bytes));
  assert.equal(result.fileID, await fileHash(result.bytes));
  const restored = await decryptFile(result.bytes, parseRecovery(JSON.stringify(result.recovery)));
  assert.equal(restored.filename, "Résumé.pdf");
  assert.deepEqual(restored.bytes, input);
  assert.ok(!new TextDecoder().decode(result.bytes).includes("Résumé.pdf"));
});

test("fresh keys and IVs produce different IDs for identical inputs", async () => {
  const a = await encryptFile(input, "a.pdf"), b = await encryptFile(input, "a.pdf");
  assert.notEqual(a.fileID, b.fileID);
  assert.notEqual(a.recovery.key, b.recovery.key);
});

test("wrong key is rejected", async () => {
  const a = await encryptFile(input, "a.pdf"), b = await encryptFile(input, "b.pdf");
  await assert.rejects(decryptFile(a.bytes, { ...a.recovery, key: b.recovery.key }), /Wrong recovery key/);
});

test("ciphertext and IV tampering fail authentication even with a recomputed hash", async () => {
  const a = await encryptFile(input, "a.pdf");
  for (const offset of [9, 21, a.bytes.byteLength - 1]) {
    const altered = a.bytes.slice(0);
    new Uint8Array(altered)[offset] ^= 1;
    await assert.rejects(decryptFile(altered, { ...a.recovery, file_id: await fileHash(altered) }), /Download blocked/);
  }
});

test("wrong file, unsupported version, and truncated data are rejected", async () => {
  const a = await encryptFile(input, "a.pdf"), b = await encryptFile(input, "b.pdf");
  await assert.rejects(decryptFile(a.bytes, b.recovery));
  await assert.rejects(decryptFile(a.bytes.slice(0, 22), a.recovery));
  const changed = a.bytes.slice(0); new Uint8Array(changed)[8] = 2;
  await assert.rejects(decryptFile(changed, a.recovery), /unsupported/);
});

test("invalid recovery files and unsafe names are rejected", async () => {
  for (const text of ["{}", "null", "x".repeat(2049), '{"version":2}']) assert.throws(() => parseRecovery(text));
  for (const name of ["../a.pdf", "a\\b.pdf", "bad\n.pdf", "a".repeat(256)]) await assert.rejects(encryptFile(input, name));
  await assert.rejects(encryptFile(new ArrayBuffer(0), "a.pdf"));
  await assert.rejects(encryptFile(new ArrayBuffer(MAX_PLAIN_BYTES + 1), "a.pdf"));
});

test("largest permitted file remains within the storage limit", async () => {
  const input = new ArrayBuffer(MAX_PLAIN_BYTES);
  const result = await encryptFile(input, "a.pdf");
  assert.ok(result.bytes.byteLength <= 16 * 1024 * 1024);
  assert.equal((await decryptFile(result.bytes, result.recovery)).bytes.byteLength, MAX_PLAIN_BYTES);
});

TANK_TEST

cat > docs/browser-encryption.md <<'TANK_DOC'
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

TANK_DOC

python3 - <<'PY_PATCH'
from pathlib import Path

p = Path("frontend/src/components/workspace.tsx")
s = p.read_text()
if 'from "@/lib/file-crypto"' in s:
    raise SystemExit("Encryption is already connected; do not apply this patch twice.")

def replace(old, new):
    global s
    if s.count(old) != 1:
        raise SystemExit(f"Expected one occurrence of: {old[:100]!r}. No changes written.")
    s = s.replace(old, new, 1)

replace('import { useEffect, useState } from "react";', '''import { useEffect, useState } from "react";
import {
  encryptFile, decryptFile, isEncrypted, parseRecovery, MAX_PLAIN_BYTES,
  type Recovery,
} from "@/lib/file-crypto";''')

replace('  if (!response.ok) {', '''  if (!response.ok) {
    if (response.status === 401) {
      window.dispatchEvent(new Event("tank:session-expired"));
    }''')

replace('export default function Workspace() {', '''function download(bytes: ArrayBuffer, filename: string) {
  const url = URL.createObjectURL(new Blob([bytes], { type: "application/octet-stream" }));
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  link.remove();
  window.setTimeout(() => URL.revokeObjectURL(url), 30_000);
}

export default function Workspace() {
  const [prepared, setPrepared] = useState<Awaited<ReturnType<typeof encryptFile>> | null>(null);
  const [recovery, setRecovery] = useState<Recovery | null>(null);
  const [keyDownloaded, setKeyDownloaded] = useState(false);
  const [keySaved, setKeySaved] = useState(false);''')

replace('    setFile(null);', '''    setFile(null);
    setPrepared(null);
    setKeyDownloaded(false);
    setKeySaved(false);''')
replace('selected.size > MAX_BYTES', 'selected.size > MAX_PLAIN_BYTES')
replace('setError("Choose a file between 1 byte and 16 MiB.");', 'setError("Choose a file between 1 byte and 16 MiB minus 4.1 KiB for encryption overhead.");')

start = s.index('  async function tankFile() {')
end = s.index('  async function retrieveFile() {', start)
s = s[:start] + '''  function downloadRecovery() {
    if (!prepared) return;
    const bytes = new TextEncoder().encode(JSON.stringify(prepared.recovery, null, 2)).buffer;
    download(bytes, `${prepared.fileID}.tank-key.json`);
    setKeyDownloaded(true);
    setMessage("Check your downloads and keep the recovery key somewhere private.");
  }

  async function loadRecovery(selected: File | null) {
    if (!selected) return;
    setRecovery(null);
    setError("");
    try {
      if (selected.size > 2048) throw new Error("Recovery file exceeds 2 KiB.");
      const key = parseRecovery(await selected.text());
      setRecovery(key);
      setFileID(key.file_id);
      setRegistration(null);
      setMessage("Recovery key loaded in this tab. It will not be sent to storage.");
    } catch (error) {
      setError(error instanceof Error ? error.message : "Invalid recovery file.");
    }
  }

  async function tankFile() {
    if (!file) return;
    await run("tank", async () => {
      if (!prepared) {
        const result = await encryptFile(await file.arrayBuffer(), file.name);
        setPrepared(result);
        setKeyDownloaded(false);
        setKeySaved(false);
        setMessage("Encrypted in your browser. Save the recovery key before tanking.");
        return;
      }
      if (!keyDownloaded || !keySaved) throw new Error("Save your recovery key first.");
      const response = await api("files", {
        method: "POST",
        headers: {
          "Content-Type": "application/octet-stream",
          "X-Tank-Filename": `${prepared.fileID}.tankenc`,
        },
        body: prepared.bytes,
      });
      const result = await response.json();
      if (result.file_id !== prepared.fileID || result.size !== prepared.bytes.byteLength) {
        throw new Error("The tanking response did not match your encrypted file.");
      }
      setRecovery(prepared.recovery);
      setFileID(result.file_id);
      setRegistration(null);
      setIDs((previous) => [result.file_id, ...previous.filter((id) => id !== result.file_id)].slice(0, 100));
      setPrepared(null);
      setFile(null);
      setMessage("Encrypted file tanked. Keep its recovery key to retrieve it later.");
    });
  }

''' + s[end:]

start = s.index('      const url = URL.createObjectURL(', s.index('  async function retrieveFile()'))
end = s.index('\n    });', start)
s = s[:start] + '''      const encodedName = response.headers.get("X-Tank-Filename");
      const storedName = encodedName ? decodeURIComponent(encodedName) : `${id}.bin`;
      if (isEncrypted(bytes) || storedName.endsWith(".tankenc")) {
        if (!recovery || recovery.file_id !== id) {
          throw new Error("Choose the recovery key for this file first.");
        }
        const restored = await decryptFile(bytes, recovery);
        download(restored.bytes, restored.filename);
        setMessage("File verified, decrypted in your browser, and downloaded.");
      } else {
        download(bytes, storedName);
        setMessage("Legacy unencrypted file verified and downloaded.");
      }''' + s[end:]

replace('<small>Any file type · Up to 16 MiB</small>', '<small>Any file type · 16 MiB minus 4.1 KiB encryption overhead</small>')
replace('disabled={disabled || !file}', 'disabled={disabled || !file || Boolean(prepared && !keySaved)}')
replace('{busy === "tank" ? "Tanking…" : "Tank it"}', '{busy === "tank" ? "Working…" : prepared ? "Tank encrypted file" : "Encrypt file"}')
replace('          <button\n            className="button primary full-width"', '''          {prepared && (
            <div className="encryption-controls">
              <button type="button" className="button secondary full-width"
                disabled={Boolean(busy)} onClick={downloadRecovery}>
                Download recovery key
              </button>
              <label>
                <input type="checkbox" checked={keySaved}
                  disabled={!keyDownloaded || Boolean(busy)}
                  onChange={(event) => setKeySaved(event.target.checked)} />
                {" "}I saved the recovery key in a private place.
              </label>
            </div>
          )}

          <button
            className="button primary full-width"''')
replace('Keep another copy while evaluating the local MVP.', 'Your file and filename are encrypted before tanking. Losing the recovery key means losing access. Keep another copy during the MVP.')
replace('          <div className="verification-note">', '''          <div className="encryption-controls">
            <label className="input-label" htmlFor="recovery-key">Recovery key file</label>
            <input id="recovery-key" type="file" accept=".json"
              disabled={Boolean(busy)}
              onChange={(event) => {
                void loadRecovery(event.target.files?.[0] ?? null);
                event.target.value = "";
              }} />
            <p className="panel-note">
              {recovery ? `Key loaded for ${recovery.file_id.slice(0, 12)}…` : "Choose your .tank-key.json file to decrypt an encrypted file."}
            </p>
          </div>

          <div className="verification-note">''')
replace('Files in this coordinator. Select an ID to retrieve it.', 'Your files. Encrypted files need their matching recovery key.')
p.write_text(s)
print("PASS: browser encryption and recovery controls connected.")

PY_PATCH

cat >> frontend/src/app/globals.css <<'TANK_CSS'

/* Tank browser encryption controls */
.encryption-controls { display: grid; gap: 12px; margin: 16px 0; }
.encryption-controls label { font-size: 13px; line-height: 1.6; }
.encryption-controls input[type="checkbox"] { accent-color: #22d3ee; }
.encryption-controls input[type="file"] { width: 100%; max-width: 100%; font-size: 12px; }

TANK_CSS

cd frontend
node --experimental-strip-types --test test/file-crypto.test.mjs
npm run lint
npm run build
printf '\nPASS: browser encryption installed and production build complete.\n'
