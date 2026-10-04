import assert from "node:assert/strict";
import { randomBytes } from "node:crypto";
import { setTimeout as delay } from "node:timers/promises";
import { Tank } from "../dist/index.js";

async function main() {
  const token = process.env.TANK_API_TOKEN;
  if (!token) {
    throw new Error("TANK_API_TOKEN is missing; source .env.tank-local first");
  }

  const client = new Tank({
    baseURL: process.env.TANK_API_URL ?? "http://127.0.0.1:8080",
    token,
  });
  const signal = AbortSignal.timeout(120_000);
  const options = { signal };

  await client.health(options);
  console.log("PASS: coordinator health.");

  // Fresh data exercises a new registration on each run.
  const original = randomBytes(4096);
  const manifest = await client.tank(original, options);
  console.log(`File ID: ${manifest.file_id}`);

  const retrieved = await client.retrieve(manifest.file_id, options);
  assert.deepEqual(Buffer.from(retrieved), original);
  console.log("PASS: exact verified retrieval.");

  const ids = await client.list("", options);
  console.log(`PASS: listing returned ${ids.length} IDs on the first page.`);

  let previous = "";
  while (true) {
    const registration = await client.registrationStatus(
      manifest.file_id,
      options,
    );

    if (registration.status !== previous) {
      console.log(`Registration: ${registration.status}`);
      previous = registration.status;
    }

    if (registration.status === "failed") {
      throw new Error(
        `Registration failed: ${registration.last_error ?? "unknown error"}`,
      );
    }

    if (registration.status === "registered") {
      console.log(
        `PASS: automatic registration after ${registration.attempts} attempts.`,
      );
      if (registration.transaction_hash) {
        console.log(`Transaction: ${registration.transaction_hash}`);
      }
      break;
    }

    await delay(2000, undefined, { signal });
  }

  console.log("PASS: TypeScript SDK end-to-end demo.");
}

main().catch((error) => {
  console.error(error.message);
  process.exitCode = 1;
});
