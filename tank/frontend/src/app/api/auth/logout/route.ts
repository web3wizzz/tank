import { cookies } from "next/headers";
import {
  SESSION_COOKIE,
  SessionError,
  requireWorkspaceRequest,
  secureCookie,
} from "@/lib/auth";

export const runtime = "nodejs";

export async function POST(request: Request) {
  try {
    requireWorkspaceRequest(request);
    (await cookies()).set(SESSION_COOKIE, "", {
      httpOnly: true,
      secure: secureCookie(request),
      sameSite: "lax",
      path: "/",
      maxAge: 0,
    });
    return Response.json(
      { authenticated: false },
      { headers: { "Cache-Control": "no-store" } },
    );
  } catch (error) {
    return Response.json(
      { error: "Sign-out request not permitted." },
      {
        status: error instanceof SessionError ? error.status : 500,
        headers: { "Cache-Control": "no-store" },
      },
    );
  }
}
