import test from "node:test";
import assert from "node:assert/strict";
import { requireWorkspaceRequest, SessionError } from "../src/lib/workspace-request.ts";

const frontend = "https://tank-origin-test-3000.app.github.dev";
const base = {
  "X-Tank-Workspace": "1", "X-Tank-Origin": frontend,
  Origin: "http://localhost:3000", Host: "localhost:3000",
  "X-Forwarded-Host": new URL(frontend).host,
  "X-Forwarded-Proto": "https", "Sec-Fetch-Site": "same-origin",
};
function request(headers, method = "POST") {
  return new Request("http://localhost:3000/api/auth/session", { method, headers });
}
function configured(action) {
  const previous = process.env.TANK_FRONTEND_ORIGIN;
  process.env.TANK_FRONTEND_ORIGIN = frontend;
  try { action(); } finally {
    if (previous === undefined) delete process.env.TANK_FRONTEND_ORIGIN;
    else process.env.TANK_FRONTEND_ORIGIN = previous;
  }
}
function rejected(headers) {
  assert.throws(() => requireWorkspaceRequest(request(headers)), (error) => error instanceof SessionError && error.status === 403);
}

test("exact frontend origin and Codespaces localhost rewrite are permitted", () => configured(() => {
  assert.doesNotThrow(() => requireWorkspaceRequest(request({ ...base, Origin: frontend })));
  assert.doesNotThrow(() => requireWorkspaceRequest(request(base)));
  assert.doesNotThrow(() => requireWorkspaceRequest(request({ ...base, Origin: "http://127.0.0.1:3000", Host: "127.0.0.1:3000" })));
}));

test("rewrite requires the exact browser origin, forwarding host/protocol, and same-origin fetch", () => configured(() => {
  for (const key of ["X-Tank-Origin", "X-Forwarded-Host", "X-Forwarded-Proto", "Sec-Fetch-Site"]) {
    const headers = { ...base }; delete headers[key]; rejected(headers);
  }
  for (const overrides of [
    { "X-Tank-Origin": "https://unapproved.example" },
    { "X-Forwarded-Host": "unapproved.example" },
    { "X-Forwarded-Proto": "http" },
    { "Sec-Fetch-Site": "cross-site" },
    { "Sec-Fetch-Site": "same-site" },
    { Host: "localhost:3001" },
    { Origin: "http://remote.example:3000", Host: "remote.example:3000" },
    { Origin: "https://unapproved.example" },
    { Origin: `${frontend}.unapproved.example` },
  ]) rejected({ ...base, ...overrides });
}));

test("workspace marker and Origin remain required for state-changing requests", () => configured(() => {
  const noOrigin = { ...base }; delete noOrigin.Origin; rejected(noOrigin);
  const noMarker = { ...base }; delete noMarker["X-Tank-Workspace"]; rejected(noMarker);
  rejected({ ...base, Origin: "not-an-origin" });
}));

test("unconfigured local requests retain host matching", () => {
  const previous = process.env.TANK_FRONTEND_ORIGIN;
  delete process.env.TANK_FRONTEND_ORIGIN;
  try {
    assert.doesNotThrow(() => requireWorkspaceRequest(request({ "X-Tank-Workspace": "1", Origin: "http://localhost:3000", Host: "localhost:3000" })));
    rejected({ "X-Tank-Workspace": "1", Origin: "http://localhost:3001", Host: "localhost:3000" });
  } finally {
    if (previous !== undefined) process.env.TANK_FRONTEND_ORIGIN = previous;
  }
});
