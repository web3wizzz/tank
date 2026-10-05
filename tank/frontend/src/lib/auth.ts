import "server-only";

import { cookies } from "next/headers";
import { Tank } from "@tank-storage/sdk";
import { openSession } from "./session-crypto";

export const SESSION_COOKIE = "tank_session";

export class SessionError extends Error {
  constructor(public status: number, message: string) {
    super(message);
  }
}

export function sessionKey(): Uint8Array {
  const secret = process.env.TANK_SESSION_SECRET;
  if (!secret || !/^[0-9a-f]{64}$/.test(secret)) {
    throw new SessionError(503, "Session configuration is missing.");
  }
  return new Uint8Array(Buffer.from(secret, "hex"));
}

export function requireWorkspaceRequest(request: Request) {
  if (
    request.headers.get("X-Tank-Workspace") !== "1" ||
    request.headers.get("sec-fetch-site") === "cross-site"
  ) {
    throw new SessionError(403, "Request not permitted.");
  }

  const origin = request.headers.get("origin");
  if (request.method !== "GET" && !origin) {
    throw new SessionError(403, "Request origin is required.");
  }

  if (origin) {
    const host = (
      request.headers.get("x-forwarded-host") ??
      request.headers.get("host") ??
      new URL(request.url).host
    ).split(",")[0].trim();

    let parsed: URL;
    try {
      parsed = new URL(origin);
    } catch {
      throw new SessionError(403, "Invalid request origin.");
    }

    if (
      !["http:", "https:"].includes(parsed.protocol) ||
      parsed.host !== host
    ) {
      throw new SessionError(403, "Request origin not permitted.");
    }
  }
}

export function secureCookie(request: Request): boolean {
  return (
    new URL(request.url).protocol === "https:" ||
    request.headers.get("x-forwarded-proto")?.split(",")[0].trim() === "https" ||
    request.headers.get("origin")?.startsWith("https://") === true
  );
}

export async function sessionClient(): Promise<Tank> {
  const key = sessionKey();
  const value = (await cookies()).get(SESSION_COOKIE)?.value;
  if (!value) {
    throw new SessionError(401, "Sign in to use your storage workspace.");
  }

  let token: string;
  try {
    token = await openSession(value, key);
  } catch {
    throw new SessionError(401, "Your session expired. Sign in again.");
  }

  return new Tank({
    baseURL: process.env.TANK_API_URL ?? "http://127.0.0.1:8080",
    token,
    timeoutMs: 120_000,
  });
}
