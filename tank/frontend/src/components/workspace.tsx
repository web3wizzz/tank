"use client";

import { useEffect, useState } from "react";

const MAX_BYTES = 16 * 1024 * 1024;
const ID_PATTERN = /^[a-f0-9]{64}$/;

type Registration = {
  file_id: string;
  status: string;
  attempts: number;
  transaction_hash?: string;
};

async function api(path: string, init: RequestInit = {}) {
  const headers = new Headers(init.headers);
  headers.set("X-Tank-Workspace", "1");

  const response = await fetch(`/api/tank/${path}`, {
    ...init,
    headers,
    cache: "no-store",
    redirect: "error",
    signal: init.signal ?? AbortSignal.timeout(150_000),
  });

  if (!response.ok) {
    const result = await response.json().catch(() => null);
    throw new Error(result?.error ?? `Request failed (${response.status}).`);
  }

  return response;
}

async function digest(bytes: ArrayBuffer) {
  const hash = await crypto.subtle.digest("SHA-256", bytes);
  return Array.from(new Uint8Array(hash), (value) =>
    value.toString(16).padStart(2, "0"),
  ).join("");
}

async function readDownload(response: Response): Promise<ArrayBuffer> {
  if (!response.body) throw new Error("The download was empty.");

  const reader = response.body.getReader();
  const chunks: Uint8Array[] = [];
  let size = 0;

  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;

      size += value.byteLength;
      if (size > MAX_BYTES) {
        await reader.cancel();
        throw new Error("Download exceeds 16 MiB.");
      }
      chunks.push(value);
    }
  } finally {
    reader.releaseLock();
  }

  if (!size) throw new Error("The download was empty.");

  const bytes = new Uint8Array(size);
  let offset = 0;
  for (const chunk of chunks) {
    bytes.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return bytes.buffer;
}

export default function Workspace() {
  const [file, setFile] = useState<File | null>(null);
  const [fileID, setFileID] = useState("");
  const [ids, setIDs] = useState<string[]>([]);
  const [connected, setConnected] = useState<boolean | null>(null);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [registration, setRegistration] = useState<Registration | null>(null);

  useEffect(() => {
    const controller = new AbortController();

    async function connect() {
      try {
        await api("health", { signal: controller.signal });
        const response = await api("files", { signal: controller.signal });
        const result = await response.json();

        if (!controller.signal.aborted) {
          setConnected(true);
          setIDs(result.file_ids);
        }
      } catch (error) {
        if (!controller.signal.aborted) {
          setConnected(false);
          setError(error instanceof Error ? error.message : "Connection failed.");
        }
      }
    }

    void connect();
    return () => controller.abort();
  }, []);

  function chooseFile(selected: File | null) {
    setError("");
    setMessage("");
    setFile(null);

    if (!selected) return;
    if (selected.size < 1 || selected.size > MAX_BYTES) {
      setError("Choose a file between 1 byte and 16 MiB.");
      return;
    }

    setFile(selected);
  }

  async function run(label: string, action: () => Promise<void>) {
    setBusy(label);
    setError("");
    setMessage("");

    try {
      await action();
    } catch (error) {
      setError(error instanceof Error ? error.message : "Request failed.");
    } finally {
      setBusy("");
    }
  }

  async function refresh() {
    await run("refresh", async () => {
      try {
        await api("health");
        const response = await api("files");
        const result = await response.json();
        setIDs(result.file_ids);
        setConnected(true);
        setMessage("Storage connected. File list refreshed.");
      } catch (error) {
        setConnected(false);
        throw error;
      }
    });
  }

  async function tankFile() {
    if (!file) return;

    await run("tank", async () => {
      const bytes = await file.arrayBuffer();
      const expectedID = await digest(bytes);
      const response = await api("files", {
        method: "POST",
        headers: {
          "Content-Type": "application/octet-stream",
          "X-Tank-Filename": encodeURIComponent(file.name),
        },
        body: bytes,
      });
      const result = await response.json();

      if (result.file_id !== expectedID || result.size !== bytes.byteLength) {
        throw new Error("The tanking response did not match your file.");
      }

      setFileID(result.file_id);
      setRegistration(null);
      setIDs((previous) =>
        [result.file_id, ...previous.filter((id) => id !== result.file_id)]
          .slice(0, 100),
      );
      setMessage("File tanked. Your verified file ID is ready below.");
    });
  }

  async function retrieveFile() {
    const id = fileID.trim();

    if (!ID_PATTERN.test(id)) {
      setError("Enter a 64-character lowercase file ID.");
      return;
    }

    await run("retrieve", async () => {
      const response = await api(`files/${id}`);
      const bytes = await readDownload(response);

      if ((await digest(bytes)) !== id) {
        throw new Error("Integrity verification failed. Download blocked.");
      }

      const url = URL.createObjectURL(
        new Blob([bytes], { type: "application/octet-stream" }),
      );
      const link = document.createElement("a");
      link.href = url;
      const encodedName = response.headers.get("X-Tank-Filename");
      link.download = encodedName
        ? decodeURIComponent(encodedName)
        : `${id}.bin`;
      document.body.appendChild(link);
      link.click();
      link.remove();
      window.setTimeout(() => URL.revokeObjectURL(url), 30_000);

      setMessage("Verified file downloaded with its saved filename.");
    });
  }

  async function copyFileID() {
    const id = fileID.trim();
    if (!ID_PATTERN.test(id)) return;

    setError("");
    setMessage("");

    try {
      if (!navigator.clipboard) {
        throw new Error("Clipboard unavailable");
      }
      await navigator.clipboard.writeText(id);
      setMessage("File ID copied to clipboard.");
    } catch {
      setError("Could not copy. Select the file ID and copy it manually.");
    }
  }

  async function checkRegistration() {
    const id = fileID.trim();

    if (!ID_PATTERN.test(id)) {
      setError("Enter a 64-character lowercase file ID.");
      return;
    }

    await run("registration", async () => {
      const response = await api(`registrations/${id}`);
      const result: Registration = await response.json();

      if (result.file_id !== id) {
        throw new Error("Registration response identified a different file.");
      }

      setRegistration(result);
      setMessage("Registration status updated.");
    });
  }

  const disabled = Boolean(busy) || connected !== true;
  const validID = ID_PATTERN.test(fileID.trim());

  return (
    <section
      className="workspace"
      id="workspace"
      aria-labelledby="workspace-title"
      aria-busy={Boolean(busy)}
    >
      <div className="section-heading">
        <div>
          <span className="eyebrow">YOUR STORAGE WORKSPACE</span>
          <h2 id="workspace-title">Make room for what matters.</h2>
        </div>
        <div className="workspace-connection">
          <span className="connection-badge">
            <span className="muted-dot" aria-hidden="true" />
            {connected === null
              ? "Checking storage…"
              : connected
                ? "Storage connected"
                : "Storage unavailable"}
          </span>
          <button
            className="button secondary"
            disabled={Boolean(busy)}
            onClick={() => void refresh()}
          >
            {busy === "refresh" ? "Refreshing…" : "Refresh"}
          </button>
        </div>
      </div>

      {error && <p className="error-message" role="alert">{error}</p>}
      {message && <p className="success-message" role="status">{message}</p>}

      <div className="workspace-grid">
        <article className="panel tank-panel">
          <div className="panel-heading">
            <div>
              <h3>Tank a file</h3>
              <p>Your next file starts here.</p>
            </div>
            <span className="panel-symbol" aria-hidden="true">↗</span>
          </div>

          <label
            className={`dropzone ${file ? "has-file" : ""}`}
            onDragOver={(event) => event.preventDefault()}
            onDrop={(event) => {
              event.preventDefault();
              if (!busy) chooseFile(event.dataTransfer.files[0] ?? null);
            }}
          >
            <input
              className="visually-hidden"
              type="file"
              aria-label="Choose a file to tank"
              disabled={Boolean(busy)}
              onChange={(event) =>
                chooseFile(event.target.files?.[0] ?? null)
              }
            />
            <span className="upload-icon" aria-hidden="true">↑</span>
            <strong>{file ? file.name : "Drop your file here"}</strong>
            <span>
              {file
                ? `${(file.size / 1024).toFixed(1)} KiB · Ready to tank`
                : "or click to browse your files"}
            </span>
            <small>Any file type · Up to 16 MiB</small>
          </label>

          <button
            className="button primary full-width"
            disabled={disabled || !file}
            onClick={() => void tankFile()}
          >
            {busy === "tank" ? "Tanking…" : "Tank it"}
            <span aria-hidden="true">↗</span>
          </button>
          <p className="panel-note">
            Keep another copy while evaluating the local MVP.
          </p>
        </article>

        <article className="panel retrieve-panel">
          <div className="panel-heading">
            <div>
              <h3>Retrieve a file</h3>
              <p>Bring your bytes back.</p>
            </div>
            <span className="panel-symbol" aria-hidden="true">↙</span>
          </div>

          <label className="input-label" htmlFor="file-id">File ID</label>
          <input
            className="text-input"
            id="file-id"
            value={fileID}
            disabled={Boolean(busy)}
            onChange={(event) => {
              setFileID(event.target.value);
              setRegistration(null);
            }}
            placeholder="Paste a 64-character file ID"
            spellCheck={false}
            autoComplete="off"
          />

          <button
            type="button"
            className="button secondary full-width"
            disabled={!ID_PATTERN.test(fileID.trim())}
            onClick={() => void copyFileID()}
          >
            Copy file ID
          </button>

          <div className="verification-note">
            <span aria-hidden="true">◇</span>
            <p>Bytes are verified before the download begins.</p>
          </div>

          <button
            className="button secondary full-width"
            disabled={disabled || !validID}
            onClick={() => void retrieveFile()}
          >
            {busy === "retrieve" ? "Verifying…" : "Retrieve file"}
            <span aria-hidden="true">↓</span>
          </button>

          <button
            className="button secondary full-width"
            disabled={disabled || !validID}
            onClick={() => void checkRegistration()}
          >
            {busy === "registration" ? "Checking…" : "Check registration"}
          </button>

          {registration && (
            <div className="registration-result" aria-live="polite">
              <p>
                Status: <strong>{registration.status.replaceAll("_", " ")}</strong>
                {" · "}Attempts: {registration.attempts}
              </p>
              {registration.transaction_hash && (
                <code>{registration.transaction_hash}</code>
              )}
              <small>This reports the worker&apos;s recorded status.</small>
            </div>
          )}
        </article>
      </div>

      <article className="panel files-panel">
        <div className="panel-heading">
          <div>
            <h3>Stored files</h3>
            <p>Files in this coordinator. Select an ID to retrieve it.</p>
          </div>
          <span className="small-label">FIRST PAGE · UP TO 100 FILES</span>
        </div>

        {ids.length ? (
          <ul className="stored-file-list">
            {ids.map((id) => (
              <li key={id}>
                <button
                  className="file-id-button"
                  disabled={Boolean(busy)}
                  aria-label={`Select file ${id}`}
                  onClick={() => {
                    setFileID(id);
                    setRegistration(null);
                    setMessage("File selected. Retrieve it or check registration.");
                  }}
                >
                  <code>{id}</code>
                  <span aria-hidden="true">↗</span>
                </button>
              </li>
            ))}
          </ul>
        ) : (
          <div className="empty-state">
            <h4>{connected ? "No stored files yet." : "Connect to browse files."}</h4>
            <p>
              {connected
                ? "Tank a file to get started."
                : "Start local storage, then refresh."}
            </p>
          </div>
        )}
      </article>
    </section>
  );
}
