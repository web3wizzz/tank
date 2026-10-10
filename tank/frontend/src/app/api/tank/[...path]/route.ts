import { admitServerRequest, ServerLimitError, serverLimits } from "@/lib/server-limits";
import { APIError, IntegrityError } from "@tank-storage/sdk";
import {
  SessionError,
  requireWorkspaceRequest,
  sessionClient,
} from "@/lib/auth";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

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
      ...(status === 429 ? { "Retry-After": "1" } : {}),
    },
  });
}

function checkRequest(request: Request) {
  requireWorkspaceRequest(request);
  if (
    request.headers.get("x-tank-workspace") !== "1" ||
    request.headers.get("sec-fetch-site") === "cross-site"
  ) {
    throw new RequestError(403, "Request not permitted.");
  }
}

async function client() {
  return sessionClient();
}

function validateID(id: string | undefined): string {
  if (!id || !ID_PATTERN.test(id)) {
    throw new RequestError(400, "Use a 64-character lowercase file ID.");
  }
  return id;
}

function failure(error: unknown) {
  if (error instanceof ServerLimitError) { return json({ error: error.message }, error.status); }
  if (error instanceof SessionError) {
    return json({ error: error.message }, error.status);
  }
  if (error instanceof APIError && error.statusCode === 401) {
    return json({ error: "Sign in again. Your credential is invalid or expired." }, 401);
  }
  if (error instanceof APIError && error.statusCode === 403) {
    return json({ error: "Access not permitted." }, 403);
  }

  if (error instanceof RequestError) {
    return json({ error: error.message }, error.status);
  }

  if (error instanceof APIError && error.statusCode === 413) {
    return json({ error: "File exceeds the configured storage upload limit." }, 413);
  }
  if (error instanceof APIError && error.statusCode === 429) {
    return json({ error: "Too many requests. Wait briefly and try again." }, 429);
  }
  if (error instanceof APIError && error.statusCode === 503) {
    return json({ error: "Storage is temporarily unavailable. Try again shortly." }, 503);
  }
  if (error instanceof APIError && error.statusCode === 507) {
    return json({ error: "Storage quota or node capacity is full. Contact your administrator." }, 507);
  }
  if (error instanceof IntegrityError) {
    return json(
      { error: "Integrity verification failed. Download blocked." },
      502,
    );
  }

  if ((error instanceof Error && error.name === "TimeoutError") ||
      (error instanceof APIError && [408, 504].includes(error.statusCode))) {
    return json({ error: "Storage timed out. Try again." }, 504);
  }

  if (
    error &&
    typeof error === "object" &&
    "statusCode" in error &&
    error.statusCode === 404
  ) {
    return json({ error: "File not found." }, 404);
  }

  return json(
    { error: "Storage request failed. Check that the coordinator is running." },
    502,
  );
}

async function readFile(request: Request, maxBytes: number): Promise<Uint8Array> {
  if (request.headers.get("content-type") !== "application/octet-stream") {
    throw new RequestError(415, "Send raw file bytes.");
  }

  const declared = request.headers.get("content-length");

  if (declared !== null) {
    const length = Number(declared);
    if (!Number.isSafeInteger(length) || length < 1 || length > maxBytes) {
      throw new RequestError(413, "Choose a file within the configured upload limit.");
    }
  }

  if (!request.body) {
    throw new RequestError(400, "The file is empty.");
  }

  const reader = request.body.getReader();
  const cancel = () => { void reader.cancel().catch(() => {}); };
  request.signal.addEventListener("abort", cancel, { once: true });
  const chunks: Uint8Array[] = [];
  let length = 0;

  try {
    while (true) {
      request.signal.throwIfAborted();
      const { done, value } = await reader.read();
      request.signal.throwIfAborted();
      if (done) break;

      length += value.byteLength;

      if (length > maxBytes) {
        await reader.cancel();
        throw new RequestError(413, "The file exceeds the configured upload limit.");
      }

      chunks.push(value);
    }
  } finally {
    request.signal.removeEventListener("abort", cancel);
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
  let admission: ReturnType<typeof admitServerRequest> | undefined;
  try {
    checkRequest(request);
    admission = admitServerRequest();
    request = new Request(request, { signal: AbortSignal.any([
      request.signal, AbortSignal.timeout(admission.timeoutMs),
    ]) });
    const { path } = await context.params;

    if (path.length === 1 && path[0] === "health") {
      await (await client()).list("", { signal: request.signal });
      return json({ connected: true, max_file_bytes: serverLimits().maxFileBytes });
    }

    if (path.length === 1 && path[0] === "files") {
      const after = new URL(request.url).searchParams.get("after") ?? "";
      if (after) validateID(after);
      const ids = await (await client()).list(after, { signal: request.signal });
      return json({ file_ids: ids, next_after: ids.length === 100 ? ids[ids.length - 1] : null });
    }

    if (path.length === 2 && path[0] === "registrations") {
      const result = await (await client()).registrationStatus(validateID(path[1]), { signal: request.signal });

      return json({
        file_id: result.file_id,
        status: result.status,
        attempts: result.attempts,
        transaction_hash: result.transaction_hash,
      });
    }

    if (path.length === 2 && path[0] === "files") {
      const id = validateID(path[1]);
      const sdk = await client();
      const info = await sdk.fileInfo(id, { signal: request.signal });
      const bytes = await sdk.retrieve(id, { signal: request.signal });

      if (bytes.byteLength !== info.size) {
        throw new IntegrityError("File size differs from metadata");
      }

      const encodedName = encodeURIComponent(info.filename)
        .replace(/['()*]/g, (character) =>
          "%" + character.charCodeAt(0).toString(16).toUpperCase());

      return new Response(new Uint8Array(bytes).buffer, {
        headers: {
          "Content-Type": "application/octet-stream",
          "Content-Disposition":
            `attachment; filename="${id}.bin"; filename*=UTF-8''${encodedName}`,
          "X-Tank-Filename": encodedName,
          "Content-Length": String(bytes.byteLength),
          "Cache-Control": "no-store",
          "X-Content-Type-Options": "nosniff",
        },
      });
    }

    return json({ error: "Route not found." }, 404);
  } catch (error) {
    return failure(error);
  } finally {
    admission?.release();
  }
}

export async function POST(request: Request, context: Context) {
  let admission: ReturnType<typeof admitServerRequest> | undefined;
  try {
    checkRequest(request);
    admission = admitServerRequest();
    request = new Request(request, { signal: AbortSignal.any([
      request.signal, AbortSignal.timeout(admission.timeoutMs),
    ]) });
    const { path } = await context.params;

    if (path.length !== 1 || path[0] !== "files") {
      return json({ error: "Route not found." }, 404);
    }

    const sdk = await client();
    // Reject missing, expired, and revoked credentials before buffering a body.
    await sdk.list("", { signal: request.signal });
    const bytes = await readFile(request, admission.maxFileBytes);
    const encodedName = request.headers.get("X-Tank-Filename");
    let filename: string | undefined;

    if (encodedName !== null) {
      if (encodedName.length > 1024) {
        throw new RequestError(400, "Filename too long.");
      }
      try {
        filename = decodeURIComponent(encodedName);
      } catch {
        throw new RequestError(400, "Invalid filename encoding.");
      }
    }

    const manifest = await sdk.tank(bytes, {
      signal: request.signal,
      ...(filename === undefined ? {} : { filename }),
    });

    return json(
      { file_id: manifest.file_id, size: manifest.size },
      201,
    );
  } catch (error) {
    return failure(error);
  } finally {
    admission?.release();
  }
}
