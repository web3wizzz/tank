import test from "node:test";
import assert from "node:assert/strict";
import { admitServerRequest, serverLimits, ServerLimitError } from "../src/lib/server-limits.ts";

function settings(action) {
  const names = ["TANK_MAX_FILE_BYTES", "TANK_FRONTEND_MAX_CONCURRENT_REQUESTS", "TANK_REQUEST_TIMEOUT_SECONDS"];
  const previous = names.map((name) => process.env[name]);
  names.forEach((name) => delete process.env[name]);
  try { action(); } finally {
    names.forEach((name, index) => {
      if (previous[index] === undefined) delete process.env[name]; else process.env[name] = previous[index];
    });
  }
}
test("frontend configuration respects lower upload limits and rejects invalid bounds", () => settings(() => {
  process.env.TANK_MAX_FILE_BYTES = "4096";
  assert.equal(serverLimits().maxFileBytes, 4096);
  for (const value of ["0", "16777217", "-1", "bad", "1e3"]) {
    process.env.TANK_MAX_FILE_BYTES = value;
    assert.throws(serverLimits, (error) => error instanceof ServerLimitError && error.status === 503);
  }
}));
test("frontend request admission is bounded and idempotent release restores capacity", () => settings(() => {
  process.env.TANK_FRONTEND_MAX_CONCURRENT_REQUESTS = "1";
  const admission = admitServerRequest();
  try {
    assert.throws(admitServerRequest, (error) => error instanceof ServerLimitError && error.status === 429);
  } finally { admission.release(); admission.release(); }
  const next = admitServerRequest(); next.release();
}));
