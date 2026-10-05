import { EncryptJWT, jwtDecrypt } from "jose";

export const SESSION_SECONDS = 8 * 60 * 60;
const USER_TOKEN = /^tank_u_[0-9a-f]{64}$/;

export async function sealSession(
  token: string,
  key: Uint8Array,
  now = Math.floor(Date.now() / 1000),
): Promise<string> {
  if (!USER_TOKEN.test(token) || key.byteLength !== 32) {
    throw new TypeError("Invalid session input");
  }

  return new EncryptJWT({ token, version: 1 })
    .setProtectedHeader({ alg: "dir", enc: "A256GCM" })
    .setIssuer("tank-frontend")
    .setAudience("tank-workspace")
    .setIssuedAt(now)
    .setExpirationTime(now + SESSION_SECONDS)
    .encrypt(key);
}

export async function openSession(
  value: string,
  key: Uint8Array,
  now = Math.floor(Date.now() / 1000),
): Promise<string> {
  if (!value || value.length > 2048 || key.byteLength !== 32) {
    throw new TypeError("Invalid session");
  }

  const { payload } = await jwtDecrypt(value, key, {
    issuer: "tank-frontend",
    audience: "tank-workspace",
    keyManagementAlgorithms: ["dir"],
    contentEncryptionAlgorithms: ["A256GCM"],
    maxTokenAge: "8h",
    currentDate: new Date(now * 1000),
  });

  if (
    payload.version !== 1 ||
    typeof payload.token !== "string" ||
    !USER_TOKEN.test(payload.token)
  ) {
    throw new TypeError("Invalid session");
  }

  return payload.token;
}
