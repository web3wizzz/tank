import { randomUUID } from "node:crypto";

const replies = new Map([
  ["upload-complete", "node-stopped"],
  ["pilot-recovery-ready", "pilot-restored"],
  ["pilot-outage-ready", "pilot-outage"],
  ["pilot-recover-node", "pilot-available"],
]);
const states = new Set(["not_queued", "pending", "submitted", "registered", "failed", "unavailable"]);
const hash = /^[0-9a-f]{64}$/;
const transactionHash = /^0x[0-9a-f]{64}$/;
const requestID = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const invalid = () => new Error("Pilot coordination failed; diagnostic values withheld.");

// Build an allowlisted payload. Never spread browser key/credential objects or
// transport diagnostics into the control channel.
export function requestEvidence(type, evidence) {
  if (!replies.has(type) || typeof evidence.fileId !== "string" || !hash.test(evidence.fileId) ||
      !Number.isSafeInteger(evidence.ciphertextBytes) || evidence.ciphertextBytes < 1 ||
      !states.has(evidence.registrationStatus) || typeof evidence.transactionHash !== "string" ||
      (evidence.transactionHash !== "" && !transactionHash.test(evidence.transactionHash)) ||
      (evidence.registrationStatus === "registered" && !transactionHash.test(evidence.transactionHash))) throw invalid();
  return { protocol: 1, requestId: randomUUID(), type, fileId: evidence.fileId,
    ciphertextBytes: evidence.ciphertextBytes, registrationStatus: evidence.registrationStatus,
    transactionHash: evidence.transactionHash };
}

export function validateRequest(message) {
  if (!message || message.protocol !== 1 || typeof message.requestId !== "string" || !requestID.test(message.requestId) ||
      Object.keys(message).sort().join(",") !== "ciphertextBytes,fileId,protocol,registrationStatus,requestId,transactionHash,type") throw invalid();
  requestEvidence(message.type, message);
  return message;
}

export function replyTo(request, type, extra = {}) {
  validateRequest(request);
  if (type !== "fixture-error" && replies.get(request.type) !== type) throw invalid();
  if (Object.keys(extra).some((key) => key !== "databasePath") ||
      (extra.databasePath !== undefined && (type !== "pilot-restored" ||
       typeof extra.databasePath !== "string" || !extra.databasePath.startsWith("/") || extra.databasePath.includes("\0")))) throw invalid();
  return { protocol: 1, requestId: request.requestId, type, ...extra };
}

// Parent enforces ordering independently of the browser's own awaited steps.
export function phaseController(pilot) {
  const phases = pilot ? [...replies.keys()] : ["upload-complete"];
  const seen = new Set();
  let index = 0;
  let active;
  let failed = false;
  let bound;
  return {
    begin(message) {
      try {
        validateRequest(message);
        if (failed || active || seen.has(message.requestId) || message.type !== phases[index]) throw invalid();
        const evidence = JSON.stringify([message.fileId, message.ciphertextBytes, message.registrationStatus, message.transactionHash]);
        if (bound !== undefined && bound !== evidence) throw invalid();
        bound = evidence;
        seen.add(message.requestId);
        active = message.requestId;
      } catch { failed = true; throw invalid(); }
    },
    complete(message, type, extra) {
      if (failed || active !== message.requestId) throw invalid();
      const reply = replyTo(message, type, extra);
      if (type === "fixture-error") throw invalid();
      index++;
      active = undefined;
      return reply;
    },
    fail(message) {
      failed = true;
      active = undefined;
      return replyTo(message, "fixture-error");
    },
    get done() { return !failed && !active && index === phases.length; },
  };
}

export function pilotStep(type, evidence, timeoutMs, channel = process) {
  if (!Number.isSafeInteger(timeoutMs) || timeoutMs < 1 || timeoutMs > 90_000 ||
      !channel.connected || typeof channel.send !== "function") return Promise.reject(invalid());
  let message;
  try { message = requestEvidence(type, evidence); } catch { return Promise.reject(invalid()); }
  return new Promise((resolve, reject) => {
    const finish = (error, reply) => {
      clearTimeout(timer);
      channel.off("message", receive);
      channel.off("disconnect", disconnect);
      if (error) reject(invalid()); else resolve(reply);
    };
    const disconnect = () => finish(true);
    const receive = (reply) => {
      if (reply?.requestId !== message.requestId) return; // Late/foreign phases cannot advance the drill.
      if (reply.protocol !== 1 || reply.type !== replies.get(type) ||
          Object.keys(reply).some((key) => !["protocol", "requestId", "type", "databasePath"].includes(key)) ||
          (reply.databasePath !== undefined && (type !== "pilot-recovery-ready" ||
           typeof reply.databasePath !== "string" || !reply.databasePath.startsWith("/") || reply.databasePath.includes("\0")))) finish(true);
      else finish(false, reply);
    };
    const timer = setTimeout(() => finish(true), timeoutMs);
    channel.on("message", receive);
    channel.once("disconnect", disconnect);
    try { channel.send(message, (error) => { if (error) finish(true); }); } catch { finish(true); }
  });
}
