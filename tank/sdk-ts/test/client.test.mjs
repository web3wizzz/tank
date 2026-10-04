import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { once } from "node:events";
import { createServer } from "node:http";
import test from "node:test";

import {
  Tank,
  APIError,
  IntegrityError,
  MAX_FILE_BYTES,
} from "../dist/index.js";

const original = Buffer.from("Tank TypeScript SDK round trip");
const id = createHash("sha256").update(original).digest("hex");

const manifest = {
  version: 1,
  file_id: id,
  size: original.length,
  created_at: "2026-01-01T00:00:00Z",
  segments: [{
    index: 0,
    size: original.length,
    merkle_root: "a".repeat(64),
    shards: Array.from({ length: 6 }, (_, index) => ({
      index,
      hash: "b".repeat(64),
      node_url: "http://127.0.0.1:9101",
    })),
  }],
};

async function startServer(t, handler) {
  const server = createServer(handler);
  server.listen(0, "127.0.0.1");
  await once(server, "listening");

  t.after(() => new Promise((resolve, reject) => {
    server.close((err) => err ? reject(err) : resolve());
  }));

  return `http://127.0.0.1:${server.address().port}`;
}

test("all five methods use the expected API behavior", async (t) => {
  const baseURL = await startServer(t, async (req, res) => {
    if (req.url === "/health") {
      assert.equal(req.headers.authorization, undefined);
      res.end("ok");
      return;
    }

    assert.equal(req.headers.authorization, "Bearer test-token");

    if (req.method === "POST" && req.url === "/tank") {
      assert.equal(
        req.headers["content-type"],
        "application/octet-stream",
      );
      const chunks = [];
      for await (const chunk of req) chunks.push(chunk);
      assert.deepEqual(Buffer.concat(chunks), original);
      res.writeHead(201);
      res.end(JSON.stringify(manifest));
    } else if (req.url === `/retrieve/${id}`) {
      res.end(original);
    } else if (req.url === `/list?after=${id}`) {
      res.end(JSON.stringify([id]));
    } else if (req.url === `/registrations/${id}`) {
      res.end(JSON.stringify({
        file_id: id,
        target: "local-test",
        status: "registered",
        attempts: 1,
      }));
    } else {
      res.writeHead(404);
      res.end();
    }
  });

  const client = new Tank({ baseURL, token: "test-token" });

  await client.health();
  assert.equal((await client.tank(original)).file_id, id);
  assert.deepEqual(Buffer.from(await client.retrieve(id)), original);
  assert.deepEqual(await client.list(id), [id]);
  assert.equal(
    (await client.registrationStatus(id)).status,
    "registered",
  );
});

test("corrupted downloads are rejected", async (t) => {
  const baseURL = await startServer(t, (_, res) => {
    res.end("wrong bytes");
  });
  const client = new Tank({ baseURL, token: "token" });
  await assert.rejects(client.retrieve(id), IntegrityError);
});

test("a mismatched upload ID is rejected", async (t) => {
  const baseURL = await startServer(t, (_, res) => {
    res.end(JSON.stringify({
      ...manifest,
      file_id: "c".repeat(64),
    }));
  });
  const client = new Tank({ baseURL, token: "token" });
  await assert.rejects(client.tank(original), IntegrityError);
});

test("redirects do not contact the destination", async (t) => {
  let calls = 0;

  const destination = await startServer(t, (_, res) => {
    calls++;
    res.end();
  });

  const baseURL = await startServer(t, (_, res) => {
    res.writeHead(307, { location: destination });
    res.end();
  });

  const client = new Tank({ baseURL, token: "private-token" });

  await assert.rejects(client.retrieve(id), (err) => {
    return err instanceof APIError && err.statusCode === 307;
  });
  assert.equal(calls, 0);
});

test("invalid input and cancellation make no HTTP request", async (t) => {
  let calls = 0;
  const baseURL = await startServer(t, (_, res) => {
    calls++;
    res.end();
  });
  const client = new Tank({ baseURL, token: "token" });

  await assert.rejects(client.retrieve("../file"), TypeError);
  await assert.rejects(client.list("bad"), TypeError);
  await assert.rejects(client.registrationStatus("bad"), TypeError);
  await assert.rejects(client.tank(new Uint8Array()), RangeError);
  await assert.rejects(
    client.tank(new Uint8Array(MAX_FILE_BYTES + 1)),
    RangeError,
  );
  await assert.rejects(
    client.health({ signal: AbortSignal.abort() }),
    (err) => err.name === "AbortError",
  );
  await assert.rejects(
    new Tank({ baseURL, token: "" }).list(),
    /token/i,
  );

  assert.equal(calls, 0);
});

test("multiple JSON values are rejected", async (t) => {
  const baseURL = await startServer(t, (_, res) => res.end("[] []"));
  const client = new Tank({ baseURL, token: "token" });
  await assert.rejects(client.list(), SyntaxError);
});

test("request timeout aborts a slow response", async (t) => {
  const baseURL = await startServer(t, (_, res) => {
    const timer = setTimeout(() => res.end("ok"), 500);
    res.on("close", () => clearTimeout(timer));
  });
  const client = new Tank({
    baseURL,
    token: "token",
    timeoutMs: 25,
  });

  await assert.rejects(
    client.health(),
    (err) => err.name === "TimeoutError",
  );
});

test("oversized downloads are rejected", async (t) => {
  const baseURL = await startServer(t, (_, res) => {
    res.end(Buffer.alloc(MAX_FILE_BYTES + 1));
  });
  const client = new Tank({ baseURL, token: "token" });
  await assert.rejects(client.retrieve(id), RangeError);
});

test("registration response must identify the requested file", async (t) => {
  const baseURL = await startServer(t, (_, res) => {
    res.end(JSON.stringify({
      file_id: "d".repeat(64),
      target: "local-test",
      status: "registered",
      attempts: 1,
    }));
  });
  const client = new Tank({ baseURL, token: "token" });
  await assert.rejects(client.registrationStatus(id), TypeError);
});

test("HTTP errors are typed and response text is bounded", async (t) => {
  const baseURL = await startServer(t, (_, res) => {
    res.writeHead(401);
    res.end("x".repeat(8192));
  });
  const client = new Tank({ baseURL, token: "token" });

  await assert.rejects(client.list(), (err) => {
    assert.ok(err instanceof APIError);
    assert.equal(err.statusCode, 401);
    assert.ok(err.message.length < 4200);
    return true;
  });
});

test("invalid client configuration is rejected", () => {
  for (const baseURL of [
    "file:///tmp/tank",
    "http://user:password@localhost",
    "http://localhost/path",
    "http://localhost?token=secret",
    "http://localhost#fragment",
  ]) {
    assert.throws(
      () => new Tank({ baseURL, token: "token" }),
      TypeError,
    );
  }

  assert.throws(() => new Tank({
    baseURL: "http://localhost",
    token: "token",
    timeoutMs: 0,
  }), RangeError);

  assert.throws(() => new Tank({
    baseURL: "http://localhost",
    token: "bad\r\ntoken",
  }), TypeError);
});
