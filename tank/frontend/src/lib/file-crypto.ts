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
  try {
    const key = await crypto.subtle.importKey("raw", rawKey.buffer, "AES-GCM", false, ["encrypt"]);
    const encrypted = await crypto.subtle.encrypt({ name: "AES-GCM", iv: header.slice(9).buffer,
      additionalData: header.buffer, tagLength: 128 }, key, plain.buffer);
    const stored = new Uint8Array(HEADER_BYTES + encrypted.byteLength);
    stored.set(header);
    stored.set(new Uint8Array(encrypted), HEADER_BYTES);
    const file_id = await fileHash(stored.buffer);
    const recovery: Recovery = { version: 1, algorithm: "AES-256-GCM", file_id, key: hex(rawKey) };
    return { bytes: stored.buffer, fileID: file_id, recovery };
  } finally {
    plain.fill(0);
    rawKey.fill(0);
  }
}

export async function decryptFile(bytes: ArrayBuffer, recovery: Recovery) {
  const record = parseRecovery(JSON.stringify(recovery));
  if (bytes.byteLength > MAX_STORED_BYTES || bytes.byteLength < HEADER_BYTES + 21 ||
      !isEncrypted(bytes) || new Uint8Array(bytes)[8] !== 1) {
    throw new Error("Invalid or unsupported encrypted file.");
  }
  if (await fileHash(bytes) !== record.file_id) throw new Error("Recovery key belongs to a different file, or data is damaged.");
  const rawKey = Uint8Array.from(record.key.match(/../g)!, (b) => parseInt(b, 16));
  let key: CryptoKey;
  try {
    key = await crypto.subtle.importKey("raw", rawKey.buffer, "AES-GCM", false, ["decrypt"]);
  } finally {
    rawKey.fill(0);
  }
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

