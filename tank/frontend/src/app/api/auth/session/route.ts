import { admitServerRequest, ServerLimitError } from "@/lib/server-limits";
import { cookies } from "next/headers";
import { APIError, Tank } from "@tank-storage/sdk";
import {
  SESSION_COOKIE,
  SessionError,
  requireWorkspaceRequest,
  secureCookie,
  sessionClient,
  sessionKey,
} from "@/lib/auth";
import { sealSession, SESSION_SECONDS } from "@/lib/session-crypto";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

function json(value: unknown, status = 200) {
  return Response.json(value, {
    status,
    headers: { "Cache-Control": "no-store", ...(status === 429 ? { "Retry-After": "1" } : {}) },
  });
}

function failure(error: unknown) {
  if (error instanceof ServerLimitError) { return json({ error: error.message }, error.status); }
  if (error instanceof SessionError) {
    return json({ error: error.message }, error.status);
  }
  if (error instanceof APIError && error.statusCode === 429) {
    return json({ error: "Too many requests. Wait and try again." }, 429);
  }
  if ((error instanceof Error && error.name === "TimeoutError") ||
      (error instanceof APIError && [408, 504].includes(error.statusCode))) {
    return json({ error: "Authentication timed out. Try again." }, 504);
  }
  if (error instanceof APIError && error.statusCode === 401) {
    return json({ error: "Credential is invalid, expired, or revoked." }, 401);
  }
  return json({ error: "Authentication service unavailable." }, 503);
}

async function readToken(request: Request): Promise<string> {
  if (
    request.headers.get("content-type")?.split(";")[0].trim() !==
    "application/json"
  ) {
    throw new SessionError(415, "Send JSON.");
  }

  const reader = request.body?.getReader();
  if (!reader) throw new SessionError(400, "Credential is required.");

  const cancel = () => { void reader.cancel().catch(() => {}); };
  request.signal.addEventListener("abort", cancel, { once: true });
  const chunks: Uint8Array[] = [];
  let size = 0;
  try {
    while (true) {
      request.signal.throwIfAborted();
      const { done, value } = await reader.read();
      request.signal.throwIfAborted();
      if (done) break;
      size += value.byteLength;
      if (size > 1024) {
        throw new SessionError(413, "Request is too large.");
      }
      chunks.push(value);
    }
  } finally {
    request.signal.removeEventListener("abort", cancel);
    await reader.cancel().catch(() => {});
    reader.releaseLock();
  }

  const bytes = new Uint8Array(size);
  let offset = 0;
  for (const chunk of chunks) {
    bytes.set(chunk, offset);
    offset += chunk.byteLength;
  }

  let value: unknown;
  try {
    value = JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(bytes));
  } catch {
    throw new SessionError(400, "Invalid JSON.");
  }

  if (
    !value || typeof value !== "object" ||
    !("token" in value) || typeof value.token !== "string" ||
    !/^tank_u_[0-9a-f]{64}$/.test(value.token)
  ) {
    throw new SessionError(400, "Use an individual Tank user credential.");
  }
  return value.token;
}

export async function POST(request: Request) {
  let admission: ReturnType<typeof admitServerRequest> | undefined;
  try {
    requireWorkspaceRequest(request);
    admission = admitServerRequest();
    request = new Request(request, { signal: AbortSignal.any([
      request.signal, AbortSignal.timeout(admission.timeoutMs),
    ]) });
    const key = sessionKey();
    const token = await readToken(request);

    const sdk = new Tank({
      baseURL: process.env.TANK_API_URL ?? "http://127.0.0.1:8080",
      token,
      timeoutMs: 10_000,
    });

    // An authenticated request confirms expiry and revocation state.
    await sdk.list("", { signal: request.signal });

    const value = await sealSession(token, key);
    (await cookies()).set(SESSION_COOKIE, value, {
      httpOnly: true,
      secure: secureCookie(request),
      sameSite: "lax",
      path: "/",
      maxAge: SESSION_SECONDS,
    });

    return json({ authenticated: true });
  } catch (error) {
    return failure(error);
  } finally {
    admission?.release();
  }
}

export async function GET(request: Request) {
  let admission: ReturnType<typeof admitServerRequest> | undefined;
  try {
    requireWorkspaceRequest(request);
    admission = admitServerRequest();
    request = new Request(request, { signal: AbortSignal.any([
      request.signal, AbortSignal.timeout(admission.timeoutMs),
    ]) });
    const sdk = await sessionClient();
    await sdk.list("", { signal: request.signal });
    return json({ authenticated: true });
  } catch (error) {
    return failure(error);
  } finally {
    admission?.release();
  }
}
