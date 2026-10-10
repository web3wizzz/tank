"use client";

import { useEffect, useState } from "react";
import type { FormEvent, ReactNode } from "react";

export default function SessionGate({ children }: { children: ReactNode }) {
  const [signedIn, setSignedIn] = useState<boolean | null>(null);
  const [token, setToken] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    const controller = new AbortController();

    async function check() {
      try {
        const response = await fetch("/api/auth/session", {
          headers: { "X-Tank-Workspace": "1", "X-Tank-Origin": window.location.origin },
          cache: "no-store",
          signal: AbortSignal.any([
            controller.signal,
            AbortSignal.timeout(15_000),
          ]),
        });

        if (controller.signal.aborted) return;
        if (response.ok) {
          setSignedIn(true);
        } else {
          setSignedIn(false);
          if (response.status !== 401) {
            const result = await response.json().catch(() => null);
            setError(result?.error ?? "Could not check your session.");
          }
        }
      } catch {
        if (!controller.signal.aborted) {
          setSignedIn(false);
          setError("Could not connect. Check that local services are running.");
        }
      }
    }

    const expired = () => {
      setSignedIn(false);
      setToken("");
      setError("Your session is no longer valid. Sign in again.");
    };

    window.addEventListener("tank:session-expired", expired);
    void check();

    return () => {
      controller.abort();
      window.removeEventListener("tank:session-expired", expired);
    };
  }, []);

  async function signIn(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setError("");

    try {
      const response = await fetch("/api/auth/session", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "X-Tank-Workspace": "1",
          "X-Tank-Origin": window.location.origin,
        },
        body: JSON.stringify({ token: token.trim() }),
        signal: AbortSignal.timeout(15_000),
      });
      const result = await response.json();
      if (!response.ok) {
        throw new Error(result.error ?? "Sign-in failed.");
      }
      setToken("");
      setSignedIn(true);
    } catch (error) {
      setError(error instanceof Error ? error.message : "Sign-in failed.");
    } finally {
      setBusy(false);
    }
  }

  async function signOut() {
    setBusy(true);
    setError("");

    try {
      const response = await fetch("/api/auth/logout", {
        method: "POST",
        headers: { "X-Tank-Workspace": "1", "X-Tank-Origin": window.location.origin },
        signal: AbortSignal.timeout(15_000),
      });
      if (!response.ok) throw new Error("Sign-out failed.");
      window.dispatchEvent(new Event("tank:session-ended"));
      setSignedIn(false);
      setToken("");
    } catch (error) {
      setError(error instanceof Error ? error.message : "Sign-out failed.");
    } finally {
      setBusy(false);
    }
  }

  if (signedIn === null) {
    return (
      <section className="workspace" id="workspace">
        <p role="status">Checking your session…</p>
      </section>
    );
  }

  if (signedIn) {
    return (
      <>
        <div className="workspace-auth-bar">
          <span className="connection-badge">Signed in · Personal workspace</span>
          <button
            type="button"
            className="button secondary"
            disabled={busy}
            onClick={() => void signOut()}
          >
            {busy ? "Signing out…" : "Sign out"}
          </button>
        </div>
        {error && <p className="error-message" role="alert">{error}</p>}
        {children}
      </>
    );
  }

  return (
    <section className="workspace" id="workspace" aria-labelledby="sign-in-title">
      <div className="section-heading">
        <div>
          <span className="eyebrow">YOUR PRIVATE WORKSPACE</span>
          <h2 id="sign-in-title">Sign in to tank it.</h2>
        </div>
      </div>

      <form className="panel sign-in-panel" onSubmit={signIn}>
        <label className="input-label" htmlFor="access-credential">
          Access credential
        </label>
        <input
          className="text-input"
          id="access-credential"
          type="password"
          value={token}
          onChange={(event) => setToken(event.target.value)}
          placeholder="tank_u_…"
          autoComplete="off"
          autoCapitalize="none"
          spellCheck={false}
          disabled={busy}
          required
        />
        <p>
          Use the token from your private credential file.
          Your session lasts up to eight hours.
        </p>
        {error && <p className="error-message" role="alert">{error}</p>}
        <button
          className="button primary full-width"
          disabled={busy || !/^tank_u_[0-9a-f]{64}$/.test(token.trim())}
        >
          {busy ? "Signing in…" : "Open my workspace"}
        </button>
      </form>
    </section>
  );
}
