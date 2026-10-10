import { spawn } from "node:child_process";
import { randomBytes } from "node:crypto";
import { mkdtemp, mkdir, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { createServer } from "node:net";
import { createServer as createHTTPServer } from "node:http";
import assert from "node:assert/strict";
import { Tank } from "@tank-storage/sdk";

// Everything created by this runner belongs to a separate temporary local stack.
const frontend = fileURLToPath(new URL("..", import.meta.url));
const project = resolve(frontend, "..");
const directory = await mkdtemp(join(tmpdir(), "tank-integration-"));
const children = [];
let shuttingDown = false;
function start(command, args, options = {}) {
  const child = spawn(command, args, { cwd: project, env, stdio: "ignore", ...options });
  children.push(child);
  return child;
}
async function finished(child, label) {
  if (child.exitCode !== null) {
    if (child.exitCode !== 0) throw new Error(`${label} failed (${child.exitCode}).`);
    return;
  }
  await new Promise((resolve, reject) => {
    child.once("error", () => reject(new Error(`${label} could not start.`)));
    child.once("exit", (code) => code === 0 ? resolve() : reject(new Error(`${label} failed (${code}).`)));
  });
}
async function stop(child) {
  if (child.exitCode !== null || child.signalCode !== null) return;
  await new Promise((resolve) => {
    const timer = setTimeout(() => child.kill("SIGKILL"), 10_000);
    child.once("exit", () => { clearTimeout(timer); resolve(); });
    child.kill("SIGTERM");
  });
}
async function cleanup() {
  if (shuttingDown) return;
  shuttingDown = true;
  await Promise.all(children.map(stop));
  await rm(directory, { recursive: true, force: true });
}
for (const signal of ["SIGINT", "SIGTERM"]) {
  process.once(signal, async () => { await cleanup(); process.exit(signal === "SIGINT" ? 130 : 143); });
}
async function port() {
  const server = createServer();
  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  const value = server.address().port;
  await new Promise((resolve) => server.close(resolve));
  return value;
}
async function ready(url, child, statuses = [200]) {
  for (let attempt = 0; attempt < 100; attempt++) {
    if (child.exitCode !== null || child.signalCode !== null) throw new Error("Temporary service exited before becoming ready.");
    try {
      const response = await fetch(url, { signal: AbortSignal.timeout(1000) });
      await response.body?.cancel();
      if (statuses.includes(response.status)) return;
    } catch { /* Wait for this owned service, with a bounded startup deadline. */ }
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  throw new Error("Temporary service startup timed out.");
}
const cleanEnvironment = Object.fromEntries(Object.entries(process.env).filter(([key]) => !key.startsWith("TANK_")));
const env = { ...cleanEnvironment,
  TANK_API_TOKEN: randomBytes(32).toString("hex"),
  TANK_NODE_TOKEN: randomBytes(32).toString("hex"),
  TANK_SESSION_SECRET: randomBytes(32).toString("hex"),
  TANK_DATABASE_PATH: join(directory, "tank.sqlite"),
  TANK_MAX_SEGMENT_BYTES: "4194304",
  TANK_MAX_FILE_BYTES: "8192",
  TANK_REQUESTS_PER_MINUTE: "0",
  TANK_FRONTEND_ORIGIN: "https://tank-integration.invalid",
};
// Prevent inherited chain settings or a personal credential from reaching tests.
for (const key of Object.keys(env)) {
  if (key.startsWith("TANK_CHAIN_") || ["TANK_REGISTRY_ADDR", "TANK_REGISTRANT"].includes(key) || key.startsWith("TANK_E2E_")) delete env[key];
}
try {
  await mkdir(join(directory, "bin"));
  console.log("Building temporary storage, coordinator, and credential tools…");
  for (const [binary, command] of [["node", "tank-node"], ["coordinator", "coordinator"], ["access", "tank-access"]]) {
    await finished(start("go", ["build", "-o", join(directory, "bin", binary), `./cmd/${command}`]), `Build ${command}`);
  }
  const nodeURLs = [];
  const nodes = [];
  for (let index = 0; index < 4; index++) {
    const address = `127.0.0.1:${await port()}`;
    const child = start(join(directory, "bin", "node"), [], {
      env: { ...env, TANK_NODE_ADDR: address, TANK_NODE_DATA_DIR: join(directory, `node${index}`) },
    });
    nodeURLs.push(`http://${address}`);
    nodes.push(child);
    await ready(`http://${address}/health`, child);
  }
  env.TANK_NODES = nodeURLs.join(",");
  env.TANK_COORDINATOR_ADDR = `127.0.0.1:${await port()}`;
  env.TANK_API_URL = `http://${env.TANK_COORDINATOR_ADDR}`;
  const coordinator = start(join(directory, "bin", "coordinator"), []);
  await ready(`${env.TANK_API_URL}/health`, coordinator);
  const alice = join(directory, "alice-credential.json");
  const bob = join(directory, "bob-credential.json");
  for (const [label, output] of [["Alice integration test", alice], ["Bob integration test", bob]]) {
    await finished(start(join(directory, "bin", "access"), ["create", "--label", label, "--out", output]), "Create temporary user");
  }
  const frontendPort = await port();
  // Spawn Next directly so shutdown controls the server process, not an npm wrapper.
  const frontendEnv = { ...env };
  delete frontendEnv.TANK_API_TOKEN;
  delete frontendEnv.TANK_NODE_TOKEN;
  const server = start(process.execPath, [join(frontend, "node_modules/next/dist/bin/next"), "start", "--hostname", "127.0.0.1", "--port", String(frontendPort)], { cwd: frontend, env: frontendEnv });
  await ready(`http://127.0.0.1:${frontendPort}/api/auth/session`, server, [403]);
  const test = start(process.execPath, [join(frontend, "test/browser-encryption.e2e.mjs")], {
    cwd: frontend,
    env: { ...frontendEnv,
      TANK_E2E_CREDENTIAL_FILE: alice,
      TANK_E2E_OTHER_CREDENTIAL_FILE: bob,
      TANK_E2E_UPSTREAM_URL: `http://127.0.0.1:${frontendPort}`,
      TANK_E2E_ACCESS_TOOL: join(directory, "bin", "access"),
      TANK_E2E_REPLACEMENT_FILE: join(directory, "alice-replacement.json"),
      TANK_E2E_PAGINATION: "1",
      TANK_E2E_MAX_FILE_BYTES: "8192",
    },
    stdio: ["ignore", "inherit", "inherit", "ipc"],
  });
  let fixtures;
  test.on("message", (message) => {
    if (message?.type === "upload-complete") {
      fixtures = (async () => {
        const { token } = JSON.parse(await readFile(alice, "utf8"));
        const sdk = new Tank({ baseURL: env.TANK_API_URL, token });
        for (let index = 0; index < 100; index++) {
          await sdk.tank(new TextEncoder().encode(`Pagination fixture ${index}`), { filename: `fixture-${index}.txt` });
        }
        await stop(nodes[0]);
        if (test.connected) test.send({ type: "node-stopped" });
      })();
      fixtures.catch(() => {
        if (test.connected) test.send({ type: "fixture-error" });
      });
    }
  });
  await finished(test, "Browser integration");
  await fixtures;
  // A stalled registration upstream must honor the frontend deadline and free admission.
  const stalled = createHTTPServer((request, response) => {
    if (request.url === "/list") {
      response.writeHead(200, { "Content-Type": "application/json" });
      response.end("[]");
    }
    // Deliberately hold registration requests until frontend cancellation closes them.
  });
  await new Promise((resolve) => stalled.listen(0, "127.0.0.1", resolve));
  try {
    const deadlinePort = await port();
    const deadlineServer = start(process.execPath, [join(frontend, "node_modules/next/dist/bin/next"), "start", "--hostname", "127.0.0.1", "--port", String(deadlinePort)], {
      cwd: frontend, env: { ...frontendEnv, TANK_API_URL: `http://127.0.0.1:${stalled.address().port}`, TANK_REQUEST_TIMEOUT_SECONDS: "1", TANK_FRONTEND_MAX_CONCURRENT_REQUESTS: "1" },
    });
    const base = `http://127.0.0.1:${deadlinePort}`;
    await ready(`${base}/api/auth/session`, deadlineServer, [403]);
    const headers = { Origin: env.TANK_FRONTEND_ORIGIN, "X-Tank-Workspace": "1", "X-Tank-Origin": env.TANK_FRONTEND_ORIGIN, "Content-Type": "application/json" };
    const { token } = JSON.parse(await readFile(bob, "utf8"));
    const login = await fetch(`${base}/api/auth/session`, { method: "POST", headers, body: JSON.stringify({ token }) });
    assert.equal(login.status, 200, "Deadline fixture sign-in failed.");
    headers.Cookie = login.headers.get("set-cookie").split(";")[0];
    await login.body?.cancel();
    const started = Date.now();
    const result = await fetch(`${base}/api/tank/registrations/${"a".repeat(64)}`, { headers, signal: AbortSignal.timeout(5000) });
    assert.equal(result.status, 504);
    await result.body?.cancel();
    assert.ok(Date.now() - started < 4000, "Registration exceeded configured deadline.");
    const health = await fetch(`${base}/api/tank/health`, { headers, signal: AbortSignal.timeout(5000) });
    assert.equal(health.status, 200, "Timed-out registration retained admission.");
    await health.body?.cancel();
    await stop(deadlineServer);
    console.log("PASS: stalled registration honors frontend timeout and releases admission.");
  } finally {
    stalled.closeAllConnections();
    await new Promise((resolve) => stalled.close(resolve));
  }
  console.log("PASS: isolated local MVP integration, with temporary users and data.");
} finally {
  await cleanup();
}
