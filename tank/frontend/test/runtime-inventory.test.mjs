import test from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, mkdir, writeFile, symlink, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { checkRuntime } from "../scripts/check-runtime.mjs";

async function fixture(t) {
  const root = await mkdtemp(join(tmpdir(), "tank-runtime-check-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  await mkdir(join(root, "frontend/node_modules/next"), { recursive: true });
  await writeFile(join(root, "frontend/server.js"), "// fixture\n");
  await writeFile(join(root, "frontend/node_modules/next/package.json"), JSON.stringify({ name: "next" }));
  return root;
}

test("runtime inventory rejects copied development tooling in nested package trees", async (t) => {
  const root = await fixture(t);
  await checkRuntime(root);
  const directory = join(root, "frontend/node_modules/next/node_modules/braces");
  await mkdir(directory, { recursive: true });
  await writeFile(join(directory, "package.json"), JSON.stringify({ name: "braces" }));
  await assert.rejects(checkRuntime(root), /Build\/lint dependency in runtime: braces/);
});

test("runtime inventory rejects dependencies outside the deployment artifact", async (t) => {
  const root = await fixture(t);
  const outside = await fixture(t);
  await symlink(outside, join(root, "external"));
  await assert.rejects(checkRuntime(root), /symlink escapes artifact/);
});

test("runtime inventory requires the generated monorepo server entrypoint", async (t) => {
  const root = await fixture(t);
  await rm(join(root, "frontend/server.js"));
  await assert.rejects(checkRuntime(root), { code: "ENOENT" });
});
