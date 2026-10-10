import SessionGate from "@/components/session-gate";
import Link from "next/link";
import Workspace from "@/components/workspace";

const features = [
  {
    number: "01",
    title: "Storage that can recover.",
    description:
      "Each segment becomes four data shards and two parity shards. Tank can reconstruct it when enough verified shards remain.",
  },
  {
    number: "02",
    title: "Check the bytes you retrieve.",
    description:
      "Integrity checks help detect damaged data. The SDK verifies the retrieved file against its content identifier.",
  },
  {
    number: "03",
    title: "Keep a commitment on-chain.",
    description:
      "The local registration worker records a manifest commitment on Anvil. Your file contents stay off-chain.",
  },
];

const questions = [
  {
    question: "What is Tank?",
    answer:
      "Tank is an open-source storage MVP. It splits files across storage nodes, reconstructs missing shards, and verifies retrieved data.",
  },
  {
    question: "Can I use it for important files today?",
    answer:
      "Use the current version for local development and evaluation. Keep another copy of your files: production durability and long-term retention are not guaranteed.",
  },
  {
    question: "Are my files stored on Ethereum?",
    answer:
      "No. Storage nodes hold the file shards. The current blockchain integration records a commitment on a local Anvil chain.",
  },
  {
    question: "Are uploads encrypted?",
    answer:
      "The browser workspace encrypts file contents and the original filename before uploading. Save its recovery key privately: Tank cannot recover a lost key. SDK and CLI uploads require you to encrypt files yourself.",
  },
  {
    question: "How do developers integrate Tank?",
    answer:
      "Use the Go SDK, the TypeScript SDK, the CLI, or the HTTP API. Both SDKs have passed end-to-end tests against the local services.",
  },
];

export default function Home() {
  return (
    <main className="site-shell">
      <a className="skip-link" href="#workspace">
        Skip to storage workspace
      </a>

      <header className="navigation">
        <Link className="brand" href="/" aria-label="Tank home">
          <span className="brand-icon" aria-hidden="true">▰</span>
          tank<span className="brand-period">.</span>
        </Link>

        <nav aria-label="Main navigation">
          <a href="#features">Features</a>
          <a href="#developers">Developers</a>
          <a href="#faq">FAQ</a>
        </nav>

        <a className="button secondary nav-button" href="#workspace">
          Open workspace <span aria-hidden="true">↗</span>
        </a>
      </header>

      <section className="hero" aria-labelledby="hero-title">
        <span className="eyebrow">
          <span className="status-dot" aria-hidden="true" />
          OPEN SOURCE · LOCAL MVP
        </span>

        <h1 id="hero-title">
          Your files.<br />
          Stored with resilience.<br />
          <span className="accent-text">Retrieved with proof.</span>
        </h1>

        <p className="hero-description">
          Split files across nodes. Recover missing shards.
          Verify the bytes that come back.
        </p>

        <div className="hero-actions">
          <a className="button primary" href="#workspace">
            Explore the workspace <span aria-hidden="true">→</span>
          </a>
          <a className="button secondary" href="#developers">
            Build with Tank
          </a>
        </div>

        <p className="hero-note">
          A working local prototype, built in the open.
        </p>

        <div className="storage-diagram" aria-label="Storage process">
          <div className="diagram-step">
            <span className="diagram-icon" aria-hidden="true">↥</span>
            <strong>Your file</strong>
            <small>Split into segments</small>
          </div>

          <span className="diagram-arrow" aria-hidden="true">→</span>

          <div className="diagram-step">
            <div className="shard-grid" aria-hidden="true">
              {[0, 1, 2, 3, 4, 5].map((shard) => (
                <span
                  key={shard}
                  className={shard > 3 ? "parity-shard" : ""}
                />
              ))}
            </div>
            <strong>4 + 2 shards</strong>
            <small>For each segment</small>
          </div>

          <span className="diagram-arrow" aria-hidden="true">→</span>

          <div className="diagram-step">
            <span className="diagram-icon" aria-hidden="true">✓</span>
            <strong>Verified retrieval</strong>
            <small>Check recovered bytes</small>
          </div>
        </div>
      </section>

      <section className="metrics" aria-label="Local prototype specifications">
        <div><strong>16 MiB</strong><span>Default storage upload limit</span></div>
        <div><strong>4 + 2</strong><span>Erasure coding</span></div>
        <div><strong>2 SDKs</strong><span>Go and TypeScript</span></div>
        <div><strong>4 nodes</strong><span>Local demo configuration</span></div>
      </section>

      <section className="section-block" id="features">
        <div className="section-heading">
          <div>
            <span className="eyebrow">BUILT FOR THE DATA</span>
            <h2>More than an upload.</h2>
          </div>
          <p>
            Understand where your file goes, how it recovers,
            and what gets verified.
          </p>
        </div>

        <div className="feature-grid">
          {features.map((feature) => (
            <article className="feature-card" key={feature.number}>
              <span className="feature-number">{feature.number}</span>
              <h3>{feature.title}</h3>
              <p>{feature.description}</p>
            </article>
          ))}
        </div>
      </section>

      <section className="developer-section section-block" id="developers">
        <div className="developer-copy">
          <span className="eyebrow">FOR DEVELOPERS</span>
          <h2>Your next integration<br />starts with a file.</h2>
          <p>
            Work with a straightforward HTTP API and SDKs for Go
            and TypeScript. Upload, retrieve, list files, and
            inspect registration status.
          </p>
          <div className="technology-tags" aria-label="Project technologies">
            <span>Go</span>
            <span>TypeScript</span>
            <span>SQLite</span>
            <span>Solidity</span>
          </div>
          <a className="text-link" href="#workspace">
            Explore the local workflow <span aria-hidden="true">→</span>
          </a>
        </div>

        <div className="code-panel">
          <div className="code-heading">
            <span>TypeScript SDK</span>
            <span className="small-label">LOCAL DEVELOPMENT</span>
          </div>
          <pre><code>{`// With a configured Tank SDK client:
const manifest = await client.tank(bytes);

const recovered = await client.retrieve(
  manifest.file_id
);

const registration =
  await client.registrationStatus(
    manifest.file_id
  );`}</code></pre>
          <p>Retrieved bytes are checked by the SDK.</p>
        </div>
      </section>

      <SessionGate><Workspace /></SessionGate>

      <section className="faq-section section-block" id="faq">
        <div>
          <span className="eyebrow">A FEW THINGS TO KNOW</span>
          <h2>Clear answers.<br />Before you upload.</h2>
        </div>

        <div className="faq-list">
          {questions.map((item) => (
            <details key={item.question}>
              <summary>{item.question}</summary>
              <p>{item.answer}</p>
            </details>
          ))}
        </div>
      </section>

      <section className="closing-cta">
        <span className="eyebrow">STORE. RECOVER. VERIFY.</span>
        <h2>It starts with a file.</h2>
        <p>Explore the prototype. Help shape what comes next.</p>
        <a className="button primary" href="#workspace">
          Open the workspace <span aria-hidden="true">↗</span>
        </a>
      </section>

      <footer className="footer">
        <Link className="brand" href="/" aria-label="Tank home">
          tank<span className="brand-period">.</span>
        </Link>
        <p>Verifiable storage. Built in the open.</p>
        <span>Local MVP</span>
      </footer>
    </main>
  );
}
