import test from "node:test";
import assert from "node:assert/strict";
import { randomBytes } from "node:crypto";
import {
  sealSession,
  openSession,
  SESSION_SECONDS,
} from "../src/lib/session-crypto.ts";

const token = "tank_u_" + "a".repeat(64);
const key = randomBytes(32);
const now = 1_800_000_000;

test("sessions round-trip without exposing the raw credential", async () => {
  const value = await sealSession(token, key, now);
  assert.equal(value.includes(token), false);
  assert.equal(await openSession(value, key, now + 1), token);
});

test("each session encryption produces different ciphertext", async () => {
  assert.notEqual(
    await sealSession(token, key, now),
    await sealSession(token, key, now),
  );
});

test("expired sessions are rejected", async () => {
  const value = await sealSession(token, key, now);
  await assert.rejects(openSession(value, key, now + SESSION_SECONDS + 1));
});

test("wrong keys and tampered ciphertext are rejected", async () => {
  const value = await sealSession(token, key, now);
  await assert.rejects(openSession(value, randomBytes(32), now + 1));

  const parts = value.split(".");
  const ciphertext = Buffer.from(parts[3], "base64url");
  ciphertext[0] ^= 1;
  parts[3] = ciphertext.toString("base64url");
  await assert.rejects(openSession(parts.join("."), key, now + 1));
});

test("administrator credentials and invalid keys are rejected", async () => {
  await assert.rejects(sealSession("a".repeat(64), key, now));
  await assert.rejects(sealSession(token, randomBytes(16), now));
});
