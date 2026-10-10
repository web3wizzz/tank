export class SessionError extends Error {
  public status: number;

  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

export function requireWorkspaceRequest(request: Request) {
  if (
    request.headers.get("X-Tank-Workspace") !== "1" ||
    request.headers.get("sec-fetch-site") === "cross-site"
  ) {
    throw new SessionError(403, "Request not permitted.");
  }

  const origin = request.headers.get("origin");
  if (request.method !== "GET" && !origin) {
    throw new SessionError(403, "Request origin is required.");
  }

  if (origin) {
    const host = (
      request.headers.get("x-forwarded-host") ??
      request.headers.get("host") ??
      new URL(request.url).host
    ).split(",")[0].trim();

    let parsed: URL;
    try {
      parsed = new URL(origin);
    } catch {
      throw new SessionError(403, "Invalid request origin.");
    }

    const configured = process.env.TANK_FRONTEND_ORIGIN;
    let permitted: boolean;

    if (configured) {
      let expected: URL;
      try {
        expected = new URL(configured);
      } catch {
        throw new SessionError(503, "Frontend origin configuration is invalid.");
      }
      if (!["http:", "https:"].includes(expected.protocol)) {
        throw new SessionError(503, "Frontend origin configuration is invalid.");
      }
      permitted = parsed.origin === expected.origin;

      // Codespaces rewrites a matching browser Origin to its localhost backend.
      // Require both the browser's exact origin and a matching forwarding chain;
      // a client-supplied origin header alone never authorizes the request.
      if (!permitted) {
        const backendHost = request.headers.get("host");
        const forwardedHost = request.headers.get("x-forwarded-host")?.split(",")[0].trim();
        const forwardedProto = request.headers.get("x-forwarded-proto")?.split(",")[0].trim();
        permitted = expected.protocol === "https:" &&
          parsed.protocol === "http:" &&
          ["localhost", "127.0.0.1", "[::1]"].includes(parsed.hostname) &&
          parsed.host === backendHost &&
          forwardedHost === expected.host &&
          forwardedProto === "https" &&
          request.headers.get("sec-fetch-site") === "same-origin" &&
          request.headers.get("x-tank-origin") === expected.origin;
      }
    } else {
      permitted = ["http:", "https:"].includes(parsed.protocol) &&
        parsed.host === host;
    }

    if (!permitted) {
      throw new SessionError(403, "Request origin not permitted.");
    }
  }
}

export function secureCookie(request: Request): boolean {
  return (
    new URL(request.url).protocol === "https:" ||
    request.headers.get("x-forwarded-proto")?.split(",")[0].trim() === "https" ||
    request.headers.get("origin")?.startsWith("https://") === true
  );
}
