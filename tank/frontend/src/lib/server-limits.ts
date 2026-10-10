const MAX_FILE_BYTES = 16 * 1024 * 1024;
let active = 0;

export class ServerLimitError extends Error {
  status: number;
  constructor(status: number, message: string) { super(message); this.status = status; }
}

function integer(name: string, fallback: number, minimum: number, maximum: number): number {
  const raw = process.env[name];
  if (raw === undefined) return fallback;
  if (!/^\d+$/.test(raw) || !Number.isSafeInteger(Number(raw)) || Number(raw) < minimum || Number(raw) > maximum) {
    throw new ServerLimitError(503, "Request limit configuration is invalid.");
  }
  return Number(raw);
}

export function serverLimits() {
  return {
    maxFileBytes: integer("TANK_MAX_FILE_BYTES", MAX_FILE_BYTES, 1, MAX_FILE_BYTES),
    maxConcurrent: integer("TANK_FRONTEND_MAX_CONCURRENT_REQUESTS", 8, 1, 1024),
    timeoutMs: integer("TANK_REQUEST_TIMEOUT_SECONDS", 120, 1, 300) * 1000,
  };
}

export function admitServerRequest() {
  const bounds = serverLimits();
  if (active >= bounds.maxConcurrent) throw new ServerLimitError(429, "Too many requests. Wait briefly and try again.");
  active++;
  let released = false;
  return {
    ...bounds,
    release: () => { if (!released) { released = true; active--; } },
  };
}
