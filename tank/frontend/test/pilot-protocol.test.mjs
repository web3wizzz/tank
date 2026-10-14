import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import test from "node:test";
import { phaseController, pilotStep, replyTo, requestEvidence, validateRequest } from "./pilot-protocol.mjs";

const evidence = { fileId: "a".repeat(64), ciphertextBytes: 673, registrationStatus: "not_queued", transactionHash: "" };
class Channel extends EventEmitter {
  connected = true;
  send(message, callback) { this.message = message; callback(); }
}
test("control payload excludes recovery keys, credentials, plaintext and diagnostics", () => {
  const message = requestEvidence("upload-complete", { ...evidence, key: "private fixture key", token: "fixture credential", filename: "private filename", error: "private URL" });
  assert.equal(validateRequest(message), message);
  for (const secret of ["private fixture key", "fixture credential", "private filename", "private URL"]) assert.ok(!JSON.stringify(message).includes(secret));
  assert.throws(() => validateRequest({ ...message, key: "private fixture key" }));
});
test("bad IDs, missing confirmed hashes and invented statuses fail closed", () => {
  for (const change of [{ fileId: "not a hash" }, { fileId: { toString: () => evidence.fileId, toJSON: () => "private fixture key" } },
    { ciphertextBytes: -1 }, { registrationStatus: "registered" }, { registrationStatus: "complete" },
    { transactionHash: "provider details" }, { transactionHash: { toString: () => "0x" + "a".repeat(64), toJSON: () => "private fixture key" } }]) {
    assert.throws(() => requestEvidence("upload-complete", { ...evidence, ...change }));
  }
});
test("late phase replies cannot advance the current step", async () => {
  const channel = new Channel();
  const pending = pilotStep("upload-complete", evidence, 100, channel);
  channel.emit("message", { protocol: 1, requestId: "old-phase", type: "node-stopped" });
  channel.emit("message", replyTo(channel.message, "node-stopped"));
  assert.equal((await pending).type, "node-stopped");
  assert.equal(channel.listenerCount("message"), 0);
  assert.equal(channel.listenerCount("disconnect"), 0);
});
test("wrong correlated response and extra private fields are rejected", async () => {
  for (const change of [{ type: "pilot-restored" }, { key: "private fixture key" }, { databasePath: "/private/file" }]) {
    const channel = new Channel();
    const pending = pilotStep("upload-complete", evidence, 100, channel);
    channel.emit("message", { ...replyTo(channel.message, "node-stopped"), ...change });
    await assert.rejects(pending, /^Error: Pilot coordination failed/);
    assert.equal(channel.listenerCount("message"), 0);
  }
});
test("timeout and disconnect release listeners without retries", async () => {
  const timeout = new Channel();
  await assert.rejects(pilotStep("upload-complete", evidence, 5, timeout));
  assert.equal(timeout.listenerCount("message"), 0);
  const disconnected = new Channel();
  const pending = pilotStep("upload-complete", evidence, 100, disconnected);
  disconnected.emit("disconnect");
  await assert.rejects(pending);
  assert.equal(disconnected.listenerCount("message"), 0);
});
test("send errors and absent IPC are redacted", async () => {
  const channel = new Channel();
  channel.send = () => { throw new Error("private provider value"); };
  await assert.rejects(pilotStep("upload-complete", evidence, 100, channel), (error) => !error.message.includes("provider"));
  assert.equal(channel.listenerCount("message"), 0);
  channel.connected = false;
  await assert.rejects(pilotStep("upload-complete", evidence, 100, channel));
});
test("parent rejects concurrent phases and permanently blocks following actions", () => {
  const parent = phaseController(true);
  const upload = requestEvidence("upload-complete", evidence);
  parent.begin(upload);
  assert.throws(() => parent.begin(requestEvidence("pilot-recovery-ready", evidence)));
  assert.throws(() => parent.complete(upload, "node-stopped"));
  assert.equal(parent.done, false);
});
test("parent completes every phase once and rejects changed file/status evidence", () => {
  const parent = phaseController(true);
  for (const [type, reply] of [["upload-complete", "node-stopped"], ["pilot-recovery-ready", "pilot-restored"], ["pilot-outage-ready", "pilot-outage"], ["pilot-recover-node", "pilot-available"]]) {
    const request = requestEvidence(type, evidence);
    parent.begin(request);
    assert.equal(parent.done, false);
    parent.complete(request, reply);
  }
  assert.equal(parent.done, true);
  assert.throws(() => parent.begin(requestEvidence("pilot-recover-node", evidence)));
  assert.equal(parent.done, false);
  const changed = phaseController(true);
  const request = requestEvidence("upload-complete", evidence);
  changed.begin(request); changed.complete(request, "node-stopped");
  assert.throws(() => changed.begin(requestEvidence("pilot-recovery-ready", { ...evidence, registrationStatus: "pending" })));
  assert.equal(changed.done, false);
});
