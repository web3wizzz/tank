import { Tank, IntegrityError } from "@tank-storage/sdk";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const MAX_BYTES = 16 * 1024 * 1024;
const ID_PATTERN = /^[a-f0-9]{64}$/;

type Context = {
  params: Promise<{ path: string[] }>;
};

class RequestError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
  }
}

function json(value: unknown, status = 200) {
  return Response.json(value, {
    status,
    headers: {
      "Cache-Control": "no-store",
      "X-Content-Type-Options": "nosniff",
    },
  });
}

function checkRequest(request: Request) {
  if (
    request.headers.get("x-tank-workspace") !== "1" ||
    request.headers.get("sec-fetch-site") === "cross-site"
  ) {
    throw new RequestError(403, "Request not permitted.");
  }
}

function client() {
  const token = process.env.TANK_API_TOKEN;

  if (!token) {
    throw new RequestError(
      503,
      "Storage is not configured. Start the frontend with its launcher.",
    );
  }

  return new Tank({
    baseURL: process.env.TANK_API_URL ?? "http://127.0.0.1:8080",
    token,
    timeoutMs: 120_000,
  });
}

function validateID(id: string | undefined): string {
  if (!id || !ID_PATTERN.test(id)) {
    throw new RequestError(400, "Use a 64-character lowercase file ID.");
  }
  return id;
}

function failure(error: unknown) {
  if (error instanceof RequestError) {
    return json({ error: error.message }, error.status);
  }

  if (error instanceof IntegrityError) {
    return json(
      { error: "Integrity verification failed. Download blocked." },
      502,
    );
  }

  if (error instanceof Error && error.name === "TimeoutError") {
    return json({ error: "Storage timed out. Try again." }, 504);
  }

  if (
    error &&
    typeof error === "object" &&
    "status" in error &&
    error.status === 404
  ) {
    return json({ error: "File not found." }, 404);
  }

  return json(
    { error: "Storage request failed. Check that the coordinator is running." },
    502,
  );
}

async function readFile(request: Request): Promise<Uint8Array> {
  if (request.headers.get("content-type") !== "application/octet-stream") {
    throw new RequestError(415, "Send raw file bytes.");
  }

  const declared = request.headers.get("content-length");

  if (declared !== null) {
    const length = Number(declared);
    if (!Number.isSafeInteger(length) || length < 1 || length > MAX_BYTES) {
      throw new RequestError(413, "Choose a file between 1 byte and 16 MiB.");
    }
  }

  if (!request.body) {
    throw new RequestError(400, "The file is empty.");
  }

  const reader = request.body.getReader();
  const chunks: Uint8Array[] = [];
  let length = 0;

  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;

      length += value.byteLength;

      if (length > MAX_BYTES) {
        await reader.cancel();
        throw new RequestError(413, "The file exceeds 16 MiB.");
      }

      chunks.push(value);
    }
  } finally {
    reader.releaseLock();
  }

  if (!length) {
    throw new RequestError(400, "The file is empty.");
  }

  const bytes = new Uint8Array(length);
  let offset = 0;

  for (const chunk of chunks) {
    bytes.set(chunk, offset);
    offset += chunk.byteLength;
  }

  return bytes;
}

export async function GET(request: Request, context: Context) {
  try {
    checkRequest(request);
    const { path } = await context.params;

    if (path.length === 1 && path[0] === "health") {
      await client().health();
      return json({ connected: true });
    }

    if (path.length === 1 && path[0] === "files") {
      return json({ file_ids: await client().list() });
    }

    if (path.length === 2 && path[0] === "registrations") {
      const result = await client().registrationStatus(validateID(path[1]));

      return json({
        file_id: result.file_id,
        status: result.status,
        attempts: result.attempts,
        transaction_hash: result.transaction_hash,
      });
    }

    if (path.length === 2 && path[0] === "files") {
      const id = validateID(path[1]);
      const bytes = await client().retrieve(id);

      return new Response(new Uint8Array(bytes).buffer, {
        headers: {
          "Content-Type": "application/octet-stream",
          "Content-Disposition": `attachment; filename="${id}.bin"`,
          "Content-Length": String(bytes.byteLength),
          "Cache-Control": "no-store",
          "X-Content-Type-Options": "nosniff",
        },
      });
    }

    return json({ error: "Route not found." }, 404);
  } catch (error) {
    return failure(error);
  }
}

export async function POST(request: Request, context: Context) {
  try {
    checkRequest(request);
    const { path } = await context.params;

    if (path.length !== 1 || path[0] !== "files") {
      return json({ error: "Route not found." }, 404);
    }

    const bytes = await readFile(request);
    const manifest = await client().tank(bytes);

    return json(
      { file_id: manifest.file_id, size: manifest.size },
      201,
    );
  } catch (error) {
    return failure(error);
  }
}
