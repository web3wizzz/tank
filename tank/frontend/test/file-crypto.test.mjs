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


test("Web Crypto failure clears temporary plaintext and raw keys", async () => {
  const subtle = crypto.subtle;
  const importKey = subtle.importKey;
  const encrypt = subtle.encrypt;
  let capturedKey;
  let capturedPlain;
  try {
    subtle.importKey = async function (...args) {
      capturedKey = new Uint8Array(args[1]);
      return importKey.apply(this, args);
    };
    subtle.encrypt = async function (_algorithm, _key, data) {
      capturedPlain = new Uint8Array(data);
      throw new Error("Simulated Web Crypto encryption failure");
    };
    await assert.rejects(encryptFile(input, "private.pdf"), /Simulated Web Crypto/);
    assert.ok(capturedKey.every((byte) => byte === 0), "Temporary raw key must be cleared after encryption failure.");
    assert.ok(capturedPlain.every((byte) => byte === 0), "Temporary plaintext must be cleared after encryption failure.");
  } finally {
    subtle.importKey = importKey;
    subtle.encrypt = encrypt;
  }

  const prepared = await encryptFile(input, "private.pdf");
  try {
    subtle.importKey = async function (_format, data) {
      capturedKey = new Uint8Array(data);
      throw new Error("Simulated Web Crypto import failure");
    };
    await assert.rejects(encryptFile(input, "private.pdf"), /Simulated Web Crypto/);
    assert.ok(capturedKey.every((byte) => byte === 0), "Temporary encryption key must be cleared after import failure.");
    await assert.rejects(decryptFile(prepared.bytes, prepared.recovery), /Simulated Web Crypto/);
    assert.ok(capturedKey.every((byte) => byte === 0), "Temporary decryption key must be cleared after import failure.");
  } finally {
    subtle.importKey = importKey;
  }
});
