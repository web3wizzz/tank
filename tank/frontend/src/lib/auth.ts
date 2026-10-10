import "server-only";

import { cookies } from "next/headers";
import { Tank } from "@tank-storage/sdk";
import { openSession } from "./session-crypto";
import { SessionError } from "./workspace-request";
export { SessionError, requireWorkspaceRequest, secureCookie } from "./workspace-request";

export const SESSION_COOKIE = "tank_session";

export function sessionKey(): Uint8Array {
  const secret = process.env.TANK_SESSION_SECRET;
  if (!secret || !/^[0-9a-f]{64}$/.test(secret)) {
    throw new SessionError(503, "Session configuration is missing.");
  }
  return new Uint8Array(Buffer.from(secret, "hex"));
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
