import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { createHash } from "node:crypto";
import { chromium } from "playwright";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import http from "node:http";

// Use an existing private credential file; never log credentials or key material.
const credentialPath = process.env.TANK_E2E_CREDENTIAL_FILE;
if (!credentialPath) throw new Error("Set TANK_E2E_CREDENTIAL_FILE to a private user credential JSON file.");
async function readCredential(path) {
  try {
    const value = JSON.parse(await readFile(path, "utf8"));
    if (!/^tank_u_[0-9a-f]{64}$/.test(value?.token)) throw new Error();
    return value;
  } catch {
    throw new Error("An individual user credential JSON file is required.");
  }
}
async function fillCredential(page, value) {
  try {
    await page.getByLabel("Access credential").fill(value);
  } catch {
    throw new Error("Could not fill the workspace sign-in field.");
  }
}
const credential = await readCredential(credentialPath);
const { token } = credential;
assert.ok(/^tank_u_[0-9a-f]{64}$/.test(token), "An individual user credential is required.");
const origin = process.env.TANK_FRONTEND_ORIGIN ??
  (process.env.CODESPACE_NAME
    ? `https://${process.env.CODESPACE_NAME}-${process.env.PORT ?? 3000}.${process.env.GITHUB_CODESPACES_PORT_FORWARDING_DOMAIN ?? "app.github.dev"}`
    : "http://localhost:3000");
// Optional local transport for Codespaces behind GitHub's authentication gateway.
// Chromium still runs at the HTTPS origin, with real Web Crypto and secure cookies.
const upstream = process.env.TANK_E2E_UPSTREAM_URL;
const codespacesAuth = process.env.TANK_E2E_CODESPACES_AUTH === "1";
if (codespacesAuth) {
  const expected = `https://${process.env.CODESPACE_NAME}-${process.env.PORT ?? 3000}.${process.env.GITHUB_CODESPACES_PORT_FORWARDING_DOMAIN ?? "app.github.dev"}`;
  if (upstream || !process.env.GITHUB_TOKEN || !process.env.CODESPACE_NAME || origin !== expected) {
    throw new Error("Private Codespaces verification requires the exact current forwarded origin, GITHUB_TOKEN, and no local upstream override.");
  }
}
async function postSession(url, options) {
  // Keep gateway credentials out of Playwright's verbose request error logs.
  const headers = { ...options.headers, "Content-Type": "application/json" };
  if (codespacesAuth) headers["X-Github-Token"] = process.env.GITHUB_TOKEN;
  try {
    const response = await fetch(url, { method: "POST", headers,
      body: JSON.stringify(options.data), redirect: "manual", signal: AbortSignal.timeout(30_000) });
    await response.body?.cancel();
    return { status: () => response.status };
  } catch {
    throw new Error("Session transport verification failed.");
  }
}
const browser = await chromium.launch({ headless: true });
const context = await browser.newContext({ acceptDownloads: true });
context.setDefaultTimeout(30_000);
let uploaded;
let recovery;
const failures = [];
async function configureTransport(context) {
  if (upstream || codespacesAuth) {
    await context.route(`${origin}/**`, async (route) => {
      const request = route.request();
      const url = new URL(request.url());
      const headers = await request.allHeaders();
      // Reproduce Codespaces: loopback Host/Origin and the exact external forwarded Host.
      if (upstream) {
        headers.host = new URL(upstream).host;
        headers["x-forwarded-host"] = new URL(origin).host;
        headers["x-forwarded-proto"] = "https";
        if (headers.origin === origin) {
          // Chromium adds Fetch Metadata after route interception. Supply the
          // same-origin marker only when the initiating frame has that origin.
          if (!headers["sec-fetch-site"] && new URL(request.frame().url()).origin === origin) {
            headers["sec-fetch-site"] = "same-origin";
          }
          headers.origin = new URL(upstream).origin;
        }
      }
      if (codespacesAuth) headers["x-github-token"] = process.env.GITHUB_TOKEN;
      try {
        if (codespacesAuth) {
          // Keep the browser network stack for the gateway's cookies and consent.
          await route.continue({ headers });
          return;
        }
        const response = await route.fetch({
          url: `${upstream ?? origin}${url.pathname}${url.search}`,
          headers,
          maxRedirects: 0,
          timeout: 150_000,
        });
        await route.fulfill({ response });
      } catch {
        failures.push("Frontend forwarding failed.");
        await route.abort();
      }
    });
  }
}
async function openWorkspace(page) {
  await page.goto(origin);
  if (codespacesAuth && (await page.title()) === "Codespaces Access Port") {
    await page.getByRole("button", { name: "Continue", exact: true }).click();
  }
  await page.getByLabel("Access credential").waitFor();
}
try {
  await configureTransport(context);
  const page = await context.newPage();
  page.on("pageerror", () => failures.push("Browser JavaScript error."));
  const leakedRequests = [];
  page.on("request", (request) => {
    if (recovery && request.postDataBuffer()?.includes(Buffer.from(JSON.parse(recovery.toString()).key))) {
      leakedRequests.push("Recovery key appeared in an outgoing request.");
    }
    if (request.method() === "POST" && new URL(request.url()).pathname === "/api/tank/files") {
      uploaded = { bytes: request.postDataBuffer(), headers: request.headers() };
    }
  });
  const apiURL = upstream ?? origin;
  if (upstream) {
    const earlyRejection = await new Promise((resolve, reject) => {
      const request = http.request(`${upstream}/api/tank/files`, { method: "POST", headers: {
        Origin: origin, "X-Tank-Workspace": "1", "Content-Type": "application/octet-stream",
        "Content-Length": String(16 * 1024 * 1024),
      } }, (response) => {
        clearTimeout(timer);
        response.resume();
        resolve(response.statusCode);
        request.destroy();
      });
      const timer = setTimeout(() => { request.destroy(); reject(new Error("Unauthenticated upload waited for its body.")); }, 2000);
      request.on("error", () => { clearTimeout(timer); reject(new Error("Unauthenticated upload transport failed.")); });
      // Send only headers: the server must reject before reading any file bytes.
      request.flushHeaders();
    });
    assert.equal(earlyRejection, 401);
    console.log("PASS: unauthenticated upload is rejected before its body is buffered.");
  }
  const rejected = await postSession(`${apiURL}/api/auth/session`, {
    headers: { Origin: "https://unapproved.example", "X-Tank-Workspace": "1" },
    data: { token },
  });
  assert.equal(rejected.status(), 403, "Unapproved origins must be rejected.");
  for (const headers of [
    { "X-Tank-Workspace": "1" },
    { Origin: origin },
    { Origin: "not-an-origin", "X-Tank-Workspace": "1" },
    { Origin: `${origin}.unapproved.example`, "X-Tank-Workspace": "1" },
  ]) {
    const response = await postSession(`${apiURL}/api/auth/session`, { headers, data: { token } });
    assert.equal(response.status(), 403, "Invalid workspace requests must be rejected.");
  }
  const crossSite = await postSession(`${apiURL}/api/auth/session`, {
    headers: { Origin: origin, "X-Tank-Workspace": "1", "Sec-Fetch-Site": "cross-site" },
    data: { token },
  });
  assert.equal(crossSite.status(), 403, "Cross-site requests must be rejected.");
  await openWorkspace(page);
  await fillCredential(page, token);
  const signIn = page.waitForResponse((response) => response.url().endsWith("/api/auth/session") && response.request().method() === "POST");
  await page.getByRole("button", { name: "Open my workspace" }).click();
  const signInResponse = await signIn;
  if (signInResponse.status() !== 200) {
    const result = await signInResponse.json().catch(() => null);
    const safeErrors = ["Request not permitted.", "Request origin not permitted.", "Request origin is required.",
      "Invalid request origin.", "Frontend origin configuration is invalid.", "Session configuration is missing.",
      "Authentication service unavailable.", "Credential is invalid, expired, or revoked."];
    const reason = safeErrors.includes(result?.error) ? result.error : "Unrecognized frontend/gateway response.";
    throw new Error(`Sign-in failed (${signInResponse.status()}): ${reason}`);
  }
  await page.getByText("Storage connected", { exact: true }).waitFor();
  const cookie = (await context.cookies()).find((value) => value.name === "tank_session");
  assert.ok(cookie?.httpOnly && cookie.sameSite === "Lax");
  if (origin.startsWith("https:")) assert.ok(cookie.secure, "HTTPS sessions must use secure cookies.");
  console.log("PASS: exact origin sign-in and session cookie; unapproved/cross-site origins rejected.");

  // A valid one-page PDF, including Unicode in the original filename.
  const filename = "Tank résumé original.pdf";
  const content = "BT /F1 12 Tf 20 100 Td (Private Tank PDF) Tj ET\n";
  const objects = [
    "<< /Type /Catalog /Pages 2 0 R >>",
    "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
    "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
    `<< /Length ${Buffer.byteLength(content)} >>\nstream\n${content}endstream`,
    "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
  ];
  let pdf = "%PDF-1.4\n";
  const offsets = [0];
  for (const [index, object] of objects.entries()) {
    offsets.push(Buffer.byteLength(pdf));
    pdf += `${index + 1} 0 obj\n${object}\nendobj\n`;
  }
  const xref = Buffer.byteLength(pdf);
  pdf += `xref\n0 6\n0000000000 65535 f \n${offsets.slice(1).map((offset) => `${String(offset).padStart(10, "0")} 00000 n \n`).join("")}trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF\n`;
  const original = Buffer.from(pdf);
  await page.getByLabel("Choose a file to tank").setInputFiles({ name: filename, mimeType: "application/pdf", buffer: original });
  await page.getByRole("button", { name: "Encrypt file", exact: true }).click();
  await page.getByText("Encrypted in your browser. Save the recovery key before tanking.", { exact: true }).waitFor();
  assert.ok(await page.getByRole("button", { name: "Tank encrypted file", exact: true }).isDisabled());
  const keyDownloadPromise = page.waitForEvent("download");
  await page.getByRole("button", { name: "Download recovery key" }).click();
  const keyDownload = await keyDownloadPromise;
  const chunks = [];
  for await (const chunk of await keyDownload.createReadStream()) chunks.push(chunk);
  recovery = Buffer.concat(chunks);
  const keyData = JSON.parse(recovery.toString());
  await page.getByRole("checkbox", { name: "I saved the recovery key in a private place." }).check();
  await page.getByRole("button", { name: "Tank encrypted file", exact: true }).click();
  await page.getByText("Encrypted file tanked. Keep its recovery key to retrieve it later.", { exact: true }).waitFor();
  assert.ok(uploaded?.bytes, "The browser must upload ciphertext.");
  assert.equal(createHash("sha256").update(uploaded.bytes).digest("hex"), keyData.file_id);
  assert.equal(uploaded.headers["x-tank-filename"], `${keyData.file_id}.tankenc`);
  assert.equal(uploaded.bytes.subarray(0, 8).toString(), "TANKENC\0");
  assert.ok(!uploaded.bytes.includes(original));
  assert.ok(!uploaded.bytes.includes(Buffer.from(filename)));
  assert.ok(!uploaded.bytes.includes(Buffer.from(keyData.key)));
  console.log("PASS: browser encrypts PDF bytes and filename; storage receives an ID-named ciphertext envelope.");

  if (process.send) {
    const stopped = new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error("Test node shutdown timed out.")), 15_000);
      process.once("message", (message) => {
        clearTimeout(timer);
        if (message?.type === "node-stopped") resolve();
        else reject(new Error("Unexpected integration runner response."));
      });
    });
    process.send({ type: "upload-complete" });
    await stopped;
    console.log("Test storage node stopped; retrieval now exercises reconstruction.");
  }

  if (process.env.TANK_E2E_OTHER_CREDENTIAL_FILE) {
    const other = await readCredential(process.env.TANK_E2E_OTHER_CREDENTIAL_FILE);
    const otherContext = await browser.newContext();
    try {
      await configureTransport(otherContext);
      const otherPage = await otherContext.newPage();
      await openWorkspace(otherPage);
      await fillCredential(otherPage, other.token);
      await otherPage.getByRole("button", { name: "Open my workspace" }).click();
      await otherPage.getByText("Storage connected", { exact: true }).waitFor();
      const isolation = await otherPage.evaluate(async (id) => {
        const headers = { "X-Tank-Workspace": "1", "X-Tank-Origin": window.location.origin };
        const list = await fetch("/api/tank/files", { headers });
        const download = await fetch(`/api/tank/files/${id}`, { headers });
        return { listStatus: list.status, ids: (await list.json()).file_ids, downloadStatus: download.status };
      }, keyData.file_id);
      assert.equal(isolation.listStatus, 200);
      assert.ok(!isolation.ids.includes(keyData.file_id));
      assert.equal(isolation.downloadStatus, 404, "A second user must not retrieve the ciphertext even knowing its ID.");
      console.log("PASS: second user cannot list or retrieve another user's encrypted file.");
    } finally {
      await otherContext.close();
    }
  }

  await page.reload();
  await page.getByText("Storage connected", { exact: true }).waitFor();
  if (process.env.TANK_E2E_PAGINATION === "1") {
    assert.equal(await page.locator(".stored-file-list li").count(), 100);
    const nextPage = page.waitForResponse((response) => new URL(response.url()).pathname === "/api/tank/files" && new URL(response.url()).searchParams.has("after"));
    await page.getByRole("button", { name: "Load more files", exact: true }).click();
    assert.equal((await nextPage).status(), 200);
    await page.getByText("More stored files loaded.", { exact: true }).waitFor();
    assert.equal(await page.locator(".stored-file-list li").count(), 101);
    assert.equal(await page.getByRole("button", { name: "Load more files", exact: true }).count(), 0);
    const ids = await page.locator(".stored-file-list code").allTextContents();
    assert.equal(new Set(ids).size, 101, "Pagination must not duplicate or drop stored files.");
    const malformed = await page.evaluate(async () => (await fetch("/api/tank/files?after=invalid", {
      headers: { "X-Tank-Workspace": "1", "X-Tank-Origin": window.location.origin },
    })).status);
    assert.equal(malformed, 400);
    console.log("PASS: browser loads all 101 files across pages without duplicates and rejects malformed cursors.");
  }
  await page.getByLabel("File ID", { exact: true }).fill(keyData.file_id);
  await page.getByRole("button", { name: "Retrieve file", exact: true }).click();
  await page.getByRole("alert").filter({ hasText: "Choose the recovery key for this file first." }).waitFor();
  const wrong = { ...keyData, key: "0".repeat(64) };
  await page.getByLabel("Recovery key file").setInputFiles({ name: "wrong.tank-key.json", mimeType: "application/json", buffer: Buffer.from(JSON.stringify(wrong)) });
  await page.getByRole("button", { name: "Retrieve file", exact: true }).click();
  await page.getByRole("alert").filter({ hasText: "Wrong recovery key" }).waitFor();
  await page.getByLabel("Recovery key file").setInputFiles({ name: keyDownload.suggestedFilename(), mimeType: "application/json", buffer: recovery });
  const restoredPromise = page.waitForEvent("download");
  await page.getByRole("button", { name: "Retrieve file", exact: true }).click();
  const restored = await restoredPromise;
  assert.equal(restored.suggestedFilename(), filename);
  const restoredChunks = [];
  for await (const chunk of await restored.createReadStream()) restoredChunks.push(chunk);
  assert.ok(Buffer.concat(restoredChunks).equals(original), "Retrieved PDF must match the original bytes.");
  await page.getByText("File verified, decrypted in your browser, and downloaded.", { exact: true }).waitFor();
  assert.deepEqual(failures, []);
  assert.deepEqual(leakedRequests, []);
  console.log("PASS: missing/wrong keys fail closed; reloaded recovery restores identical PDF bytes and original Unicode filename.");
  if (upstream) {
    let release;
    let ready;
    let delivered;
    const held = new Promise((resolve) => { release = resolve; });
    const fetched = new Promise((resolve) => { ready = resolve; });
    const finished = new Promise((resolve) => { delivered = resolve; });
    const url = `${origin}/api/tank/files/${keyData.file_id}`;
    const handler = async (route) => {
      const headers = await route.request().allHeaders();
      headers.host = new URL(upstream).host;
      headers["x-forwarded-host"] = new URL(origin).host;
      headers["x-forwarded-proto"] = "https";
      const response = await route.fetch({ url: `${upstream}/api/tank/files/${keyData.file_id}`, headers });
      assert.equal(response.status(), 200, "Delayed retrieval must already have authenticated and read ciphertext.");
      ready();
      await held;
      try { await route.fulfill({ response }); } catch { /* The browser has aborted this owned request. */ }
      delivered();
    };
    await page.route(url, handler);
    await page.getByRole("button", { name: "Retrieve file", exact: true }).click();
    await fetched;
    const canceled = page.waitForEvent("requestfailed", { predicate: (request) => request.url() === url, timeout: 5000 });
    await page.getByRole("button", { name: "Sign out", exact: true }).click();
    await page.getByLabel("Access credential").waitFor();
    await canceled;
    release();
    await finished;
    await page.unroute(url, handler);
    await assert.rejects(page.waitForEvent("download", { timeout: 500 }), (error) => error.name === "TimeoutError");
    console.log("PASS: sign-out aborts a pending authenticated retrieval and prevents a late plaintext download.");
  } else {
    await page.getByRole("button", { name: "Sign out", exact: true }).click();
    await page.getByLabel("Access credential").waitFor();
  }
  assert.ok(!(await context.cookies()).some((value) => value.name === "tank_session"));
  console.log("PASS: sign-out clears the session.");
  if (process.env.TANK_E2E_ACCESS_TOOL) {
    await fillCredential(page, token);
    await page.getByRole("button", { name: "Open my workspace" }).click();
    await page.getByText("Storage connected", { exact: true }).waitFor();
    await promisify(execFile)(process.env.TANK_E2E_ACCESS_TOOL, [
      "revoke", "--db", process.env.TANK_DATABASE_PATH, "--key-id", credential.id,
    ]);
    await page.getByRole("button", { name: "Refresh", exact: true }).click();
    await page.getByLabel("Access credential").waitFor();
    const revoked = page.waitForResponse((response) => response.url().endsWith("/api/auth/session") && response.request().method() === "POST");
    await fillCredential(page, token);
    await page.getByRole("button", { name: "Open my workspace" }).click();
    assert.equal((await revoked).status(), 401);
    console.log("PASS: revocation invalidates an existing browser session and prevents new sign-in.");
    if (process.env.TANK_E2E_REPLACEMENT_FILE) {
      await promisify(execFile)(process.env.TANK_E2E_ACCESS_TOOL, [
        "issue", "--db", process.env.TANK_DATABASE_PATH,
        "--user-id", credential.principal_id, "--out", process.env.TANK_E2E_REPLACEMENT_FILE,
      ]);
      const replacement = await readCredential(process.env.TANK_E2E_REPLACEMENT_FILE);
      assert.equal(replacement.principal_id, credential.principal_id);
      await fillCredential(page, replacement.token);
      await page.getByRole("button", { name: "Open my workspace" }).click();
      await page.getByText("Storage connected", { exact: true }).waitFor();
      await page.getByLabel("Recovery key file").setInputFiles({
        name: keyDownload.suggestedFilename(), mimeType: "application/json", buffer: recovery,
      });
      const renewedDownload = page.waitForEvent("download");
      await page.getByRole("button", { name: "Retrieve file", exact: true }).click();
      const renewed = await renewedDownload;
      assert.equal(renewed.suggestedFilename(), filename);
      const renewedChunks = [];
      for await (const chunk of await renewed.createReadStream()) renewedChunks.push(chunk);
      assert.ok(Buffer.concat(renewedChunks).equals(original), "Replacement credential must preserve existing encrypted file access.");
      console.log("PASS: replacement credential signs in as the same user and restores their existing encrypted PDF.");
      await page.getByRole("button", { name: "Sign out", exact: true }).click();
      await page.getByLabel("Access credential").waitFor();
    }
  }
  console.log(upstream
    ? "Transport: local reverse-proxy simulation at the configured HTTPS browser origin."
    : codespacesAuth ? "Transport: actual private Codespaces HTTPS forwarding with gateway authentication."
    : "Transport: direct frontend URL.");
} finally {
  recovery?.fill(0);
  await context.close();
  await browser.close();
}
