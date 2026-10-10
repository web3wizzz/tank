import { spawn } from "node:child_process";
import { randomBytes } from "node:crypto";
import { mkdtemp, mkdir, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { createServer } from "node:net";

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
const env = { ...process.env,
  TANK_API_TOKEN: randomBytes(32).toString("hex"),
  TANK_NODE_TOKEN: randomBytes(32).toString("hex"),
  TANK_SESSION_SECRET: randomBytes(32).toString("hex"),
  TANK_DATABASE_PATH: join(directory, "tank.sqlite"),
  TANK_MAX_SEGMENT_BYTES: "4194304",
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
    },
    stdio: ["ignore", "inherit", "inherit", "ipc"],
  });
  test.on("message", async (message) => {
    if (message?.type === "upload-complete") {
      await stop(nodes[0]);
      if (test.connected) test.send({ type: "node-stopped" });
    }
  });
  await finished(test, "Browser integration");
  console.log("PASS: isolated local MVP integration, with temporary users and data.");
} finally {
  await cleanup();
}
