import test from "node:test";
import assert from "node:assert/strict";
import http from "node:http";
import { createHash } from "node:crypto";
import { Tank } from "../dist/index.js";

const data = new Uint8Array([1, 2, 3, 4]);
const id = createHash("sha256").update(data).digest("hex");
const hash = "a".repeat(64);

function manifest() {
  return {
    version: 1,
    file_id: id,
    size: data.length,
    created_at: "2026-10-05T00:00:00Z",
    segments: [{
      index: 0,
      size: data.length,
      merkle_root: hash,
      shards: Array.from({ length: 6 }, (_, index) => ({
        index, hash, node_url: "http://127.0.0.1:9101",
      })),
    }],
  };
}

async function mock(t, handler) {
  const server = http.createServer(handler);
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  t.after(() => new Promise((resolve) => server.close(resolve)));

  return new Tank({
    baseURL: `http://127.0.0.1:${server.address().port}`,
    token: "filename-test-token",
  });
}

test("tanking sends an encoded basename and remains backwards compatible", async (t) => {
  const received = [];
  const client = await mock(t, (request, response) => {
    received.push(request.headers["x-tank-filename"]);
    request.resume();
    response.setHeader("Content-Type", "application/json");
    response.end(JSON.stringify(manifest()));
  });

  await client.tank(data, { filename: "C:\\fakepath\\résumé.pdf" });
  await client.tank(data);

  assert.deepEqual(received, [encodeURIComponent("résumé.pdf"), undefined]);
});

test("invalid filenames make no HTTP requests", async (t) => {
  let requests = 0;
  const client = await mock(t, (_request, response) => {
    requests++;
    response.end("{}");
  });

  for (const filename of ["", "..", "bad\r\nname.pdf", "x".repeat(256)]) {
    await assert.rejects(client.tank(data, { filename }), TypeError);
  }
  assert.equal(requests, 0);
});

test("fileInfo preserves a Unicode filename", async (t) => {
  let requestedPath;
  const client = await mock(t, (request, response) => {
    requestedPath = request.url;
    response.end(JSON.stringify({
      file_id: id, filename: "报告.pdf", size: data.length,
    }));
  });

  assert.deepEqual(await client.fileInfo(id), {
    file_id: id, filename: "报告.pdf", size: data.length,
  });
  assert.equal(requestedPath, `/files/${id}/info`);
});

test("fileInfo rejects mismatched IDs and unsafe filenames", async (t) => {
  let filename = "report.pdf";
  let fileID = "b".repeat(64);
  const client = await mock(t, (_request, response) => {
    response.end(JSON.stringify({
      file_id: fileID, filename, size: data.length,
    }));
  });

  await assert.rejects(client.fileInfo(id), TypeError);

  fileID = id;
  filename = "../report.pdf";
  await assert.rejects(client.fileInfo(id), TypeError);
});
