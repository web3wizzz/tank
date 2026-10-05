import type {
  Manifest,
  Registration,
  RequestOptions,
  TankConfig,
  TankOptions,
  FileInfo,
} from "./types.js";

export const MAX_FILE_BYTES = 16 * 1024 * 1024;
const MAX_JSON_BYTES = 8 * 1024 * 1024;
const FILE_ID = /^[0-9a-f]{64}$/;
const STATES = new Set([
  "not_queued", "pending", "submitted", "registered", "failed",
]);

export class APIError extends Error {
  readonly statusCode: number;

  constructor(statusCode: number, message: string) {
    super(`Tank API returned HTTP ${statusCode}: ${message}`);
    this.name = "APIError";
    this.statusCode = statusCode;
  }
}

export class IntegrityError extends Error {
  constructor(message = "Content failed integrity verification") {
    super(message);
    this.name = "IntegrityError";
  }
}


function normalizeFilename(value: string): string {
  if (typeof value !== "string") {
    throw new TypeError("Filename must be a string");
  }

  for (const character of value) {
    const point = character.codePointAt(0)!;
    if (
      point < 32 || (point >= 127 && point <= 159) ||
      (point >= 0xd800 && point <= 0xdfff) ||
      (point >= 0x202a && point <= 0x202e) ||
      (point >= 0x2066 && point <= 0x2069)
    ) {
      throw new TypeError("Invalid filename");
    }
  }

  const name = value.trim().replaceAll("\\", "/")
    .replace(/\/+$/, "").split("/").at(-1) ?? "";

  if (
    !name || name === "." || name === ".." ||
    new TextEncoder().encode(name).byteLength > 255
  ) {
    throw new TypeError("Invalid filename");
  }
  return name;
}

function requireID(id: string): void {
  if (!FILE_ID.test(id)) {
    throw new TypeError("File ID must be 64 lowercase hexadecimal characters");
  }
}

function record(value: unknown): Record<string, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new TypeError("API returned an invalid object");
  }
  return value as Record<string, unknown>;
}

function parseJSON(data: Uint8Array): unknown {
  return JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(data));
}

async function digest(data: Uint8Array): Promise<string> {
  const hash = await crypto.subtle.digest(
    "SHA-256", new Uint8Array(data).buffer,
  );
  return Array.from(
    new Uint8Array(hash),
    (n) => n.toString(16).padStart(2, "0"),
  ).join("");
}

async function readLimited(
  response: Response,
  limit: number,
  truncate = false,
): Promise<Uint8Array> {
  if (!response.body) return new Uint8Array();

  const reader = response.body.getReader();
  const chunks: Uint8Array[] = [];
  let size = 0;

  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;

      if (size + value.byteLength > limit) {
        if (!truncate) {
          throw new RangeError("API response exceeds size limit");
        }
        chunks.push(value.subarray(0, limit - size));
        size = limit;
        break;
      }

      chunks.push(value);
      size += value.byteLength;
    }
  } finally {
    await reader.cancel().catch(() => {});
    reader.releaseLock();
  }

  const data = new Uint8Array(size);
  let offset = 0;
  for (const chunk of chunks) {
    data.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return data;
}

function parseManifest(
  value: unknown, id: string, size: number,
): Manifest {
  const m = record(value);

  if (m.file_id !== id) {
    throw new IntegrityError("Returned file ID differs from submitted bytes");
  }

  if (
    m.version !== 1 || m.size !== size ||
    typeof m.created_at !== "string" ||
    !Number.isFinite(Date.parse(m.created_at)) ||
    !Array.isArray(m.segments) || m.segments.length === 0
  ) {
    throw new TypeError("API returned an invalid manifest");
  }

  let total = 0;
  for (const [index, value] of m.segments.entries()) {
    const segment = record(value);

    if (
      segment.index !== index ||
      typeof segment.size !== "number" ||
      !Number.isSafeInteger(segment.size) ||
      segment.size < 1 || segment.size > MAX_FILE_BYTES ||
      typeof segment.merkle_root !== "string" ||
      !FILE_ID.test(segment.merkle_root) ||
      !Array.isArray(segment.shards) || segment.shards.length !== 6
    ) {
      throw new TypeError("API returned an invalid segment");
    }

    total += segment.size;

    for (const [shardIndex, value] of segment.shards.entries()) {
      const shard = record(value);
      if (
        shard.index !== shardIndex ||
        typeof shard.hash !== "string" ||
        !FILE_ID.test(shard.hash) ||
        typeof shard.node_url !== "string"
      ) {
        throw new TypeError("API returned an invalid shard");
      }
    }
  }

  if (total !== size) {
    throw new TypeError("Manifest segment sizes do not match file size");
  }

  return value as Manifest;
}

function parseRegistration(value: unknown, id: string): Registration {
  const s = record(value);

  if (
    s.file_id !== id ||
    typeof s.target !== "string" || s.target.length === 0 ||
    typeof s.status !== "string" || !STATES.has(s.status) ||
    typeof s.attempts !== "number" ||
    !Number.isSafeInteger(s.attempts) || s.attempts < 0
  ) {
    throw new TypeError("API returned an invalid registration response");
  }

  if (
    s.transaction_hash !== undefined &&
    (
      typeof s.transaction_hash !== "string" ||
      (
        s.transaction_hash !== "" &&
        !/^0x[0-9a-f]{64}$/.test(s.transaction_hash)
      )
    )
  ) {
    throw new TypeError("API returned an invalid transaction hash");
  }

  if (s.last_error !== undefined && typeof s.last_error !== "string") {
    throw new TypeError("API returned an invalid registration error");
  }

  return value as Registration;
}

export class Tank {
  private readonly baseURL: string;
  private readonly token: string;
  private readonly timeoutMs: number;

  constructor(config: TankConfig) {
    const url = new URL(config.baseURL);

    if (
      (url.protocol !== "http:" && url.protocol !== "https:") ||
      url.username || url.password || url.search || url.hash ||
      url.pathname !== "/"
    ) {
      throw new TypeError("Coordinator URL must be an HTTP or HTTPS server URL");
    }

    if (typeof config.token !== "string" || /[\r\n]/.test(config.token)) {
      throw new TypeError("Invalid API token");
    }

    const timeout = config.timeoutMs ?? 120_000;
    if (
      !Number.isSafeInteger(timeout) ||
      timeout < 1 || timeout > 2_147_483_647
    ) {
      throw new RangeError(
        "timeoutMs must be a positive integer no greater than 2147483647",
      );
    }

    this.baseURL = url.origin;
    this.token = config.token;
    this.timeoutMs = timeout;
  }

  private async request(
    method: string,
    path: string,
    authenticated: boolean,
    options: RequestOptions,
    limit: number,
    body?: ArrayBuffer,
    filename?: string,
  ): Promise<Uint8Array> {
    if (authenticated && !this.token) {
      throw new Error("An API token is required");
    }

    const timeout = AbortSignal.timeout(this.timeoutMs);
    const signal = options.signal
      ? AbortSignal.any([options.signal, timeout])
      : timeout;

    signal.throwIfAborted();

    const headers = new Headers();
    if (authenticated) {
      headers.set("Authorization", `Bearer ${this.token}`);
    }

    if (filename !== undefined) {
      headers.set("X-Tank-Filename", encodeURIComponent(filename));
    }

    const init: RequestInit = {
      method,
      headers,
      signal,
      redirect: "manual",
    };

    if (body !== undefined) {
      headers.set("Content-Type", "application/octet-stream");
      init.body = body;
    }

    const response = await fetch(this.baseURL + path, init);

    if (!response.ok) {
      const message = new TextDecoder()
        .decode(await readLimited(response, 4096, true))
        .trim();
      throw new APIError(response.status, message);
    }

    const data = await readLimited(response, limit);
    signal.throwIfAborted();
    return data;
  }

  async health(options: RequestOptions = {}): Promise<void> {
    await this.request("GET", "/health", false, options, 4096);
  }

  async tank(
    data: Uint8Array,
    options: TankOptions = {},
  ): Promise<Manifest> {
    if (!(data instanceof Uint8Array)) {
      throw new TypeError("File data must be a Uint8Array");
    }
    if (data.byteLength < 1 || data.byteLength > MAX_FILE_BYTES) {
      throw new RangeError("File must contain 1 byte to 16 MiB");
    }

    options.signal?.throwIfAborted();

    const filename = options.filename === undefined
      ? undefined
      : normalizeFilename(options.filename);

    const payload = new Uint8Array(data);
    const id = await digest(payload);
    const response = await this.request(
      "POST", "/tank", true, options, MAX_JSON_BYTES, payload.buffer, filename,
    );

    return parseManifest(parseJSON(response), id, payload.byteLength);
  }

  async retrieve(
    id: string,
    options: RequestOptions = {},
  ): Promise<Uint8Array> {
    requireID(id);

    const data = await this.request(
      "GET", `/retrieve/${id}`, true, options, MAX_FILE_BYTES,
    );

    if (data.byteLength === 0) {
      throw new RangeError("Retrieved file is empty");
    }
    if (await digest(data) !== id) {
      throw new IntegrityError();
    }

    options.signal?.throwIfAborted();
    return data;
  }

  async list(
    after = "",
    options: RequestOptions = {},
  ): Promise<string[]> {
    if (after !== "") requireID(after);

    const path = after
      ? `/list?after=${encodeURIComponent(after)}`
      : "/list";

    const value = parseJSON(
      await this.request("GET", path, true, options, MAX_JSON_BYTES),
    );

    if (value === null) return [];

    if (
      !Array.isArray(value) || value.length > 100 ||
      !value.every(
        (id: unknown) => typeof id === "string" && FILE_ID.test(id),
      )
    ) {
      throw new TypeError("API returned an invalid file list");
    }

    return value as string[];
  }


  async fileInfo(
    id: string,
    options: RequestOptions = {},
  ): Promise<FileInfo> {
    requireID(id);

    const response = await this.request(
      "GET", `/files/${id}/info`, true, options, 4096,
    );
    const info = record(parseJSON(response));
    const filename = typeof info.filename === "string"
      ? normalizeFilename(info.filename)
      : "";

    if (
      info.file_id !== id || !filename || filename !== info.filename ||
      typeof info.size !== "number" ||
      !Number.isSafeInteger(info.size) ||
      info.size < 1 || info.size > MAX_FILE_BYTES
    ) {
      throw new TypeError("API returned invalid file information");
    }

    return { file_id: id, filename, size: info.size };
  }

  async registrationStatus(
    id: string,
    options: RequestOptions = {},
  ): Promise<Registration> {
    requireID(id);

    const response = await this.request(
      "GET", `/registrations/${id}`, true, options, MAX_JSON_BYTES,
    );

    return parseRegistration(parseJSON(response), id);
  }
}
