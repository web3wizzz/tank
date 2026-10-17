import assert from "node:assert/strict";
import { readdir, readFile, realpath, stat } from "node:fs/promises";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

// This list includes the unresolved braces lint chain and unnecessary build/test
// tooling. A production-only npm audit cannot inspect copied image contents.
const excluded = new Set([
  "braces", "micromatch", "fast-glob", "eslint", "eslint-config-next",
  "@next/eslint-plugin-next", "typescript", "playwright", "playwright-core",
  "tailwindcss", "@tailwindcss/postcss",
]);

export async function checkRuntime(directory) {
  const root = await realpath(directory);
  const seen = new Set();
  const packages = new Set();
  async function walk(path) {
    const actual = await realpath(path);
    const rel = relative(root, actual);
    assert.ok(rel !== ".." && !rel.startsWith("../"), "Runtime symlink escapes artifact");
    if (seen.has(actual)) return;
    seen.add(actual);
    if (!(await stat(actual)).isDirectory()) return;
    for (const entry of await readdir(actual, { withFileTypes: true })) {
      const child = join(actual, entry.name);
      if (entry.name === "package.json" && entry.isFile()) {
        const manifest = JSON.parse(await readFile(child, "utf8"));
        if (manifest.name) {
          packages.add(manifest.name);
          assert.ok(!excluded.has(manifest.name), `Build/lint dependency in runtime: ${manifest.name}`);
        }
      } else if (entry.isDirectory() || entry.isSymbolicLink()) {
        await walk(child);
      }
    }
  }
  await walk(root);
  assert.ok(packages.has("next"), "Next runtime package is missing");
  await stat(join(root, "frontend", "server.js"));
  console.log(`PASS: traced runtime inventory (${packages.size} packages), no excluded build/lint tooling.`);
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const directory = process.argv[2] ?? resolve(dirname(fileURLToPath(import.meta.url)), "../.next/standalone");
  await checkRuntime(directory);
}
