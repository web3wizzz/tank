import { ArrowUpRight, Braces, CheckCircle2, CircleDashed, Code2, Database, GitBranch, Network, ServerCog, ShieldCheck } from "lucide-react";
import FeatureSection from "@/components/feature-section";
import Hero from "@/components/hero";
import Logo from "@/components/logo";
import Navbar from "@/components/navbar";
import Reveal from "@/components/reveal";
import SessionGate from "@/components/session-gate";
import Workspace from "@/components/workspace";

const repo = "https://github.com/web3wizzz/tank";

const stack = [
  { name: "Go", role: "Coordinator and node services", icon: ServerCog },
  { name: "TypeScript", role: "Browser workspace and SDK", icon: Braces },
  { name: "SQLite", role: "Coordinator metadata", icon: Database },
  { name: "Solidity", role: "Manifest commitment registry", icon: Code2 },
];

const stages = [
  { label: "Available", title: "Local MVP", icon: CheckCircle2, body: "Run the coordinator, four local storage processes, registration worker, and this encrypted browser workspace on your own machine.", items: ["Browser encryption", "4 + 2 erasure coding", "Per-user isolation", "Recovery drill tests"] },
  { label: "In validation", title: "Testnet pilot", icon: Network, body: "The public Arc Testnet registry is deployed and a signed encrypted-file commitment was verified. Independent hosted storage remains outstanding.", items: ["Public registry deployed", "Signed registration verified", "Hosted acceptance outstanding"] },
  { label: "Planned", title: "Future production", icon: CircleDashed, body: "Long-running independent operators, public HTTPS services, monitoring, funding, and explicit retention terms require future work.", items: ["Independent operators", "Published service terms", "Sustained availability"] },
];

const questions = [
  { question: "What does “tank it” mean?", answer: "It means encrypting a file in this browser, uploading its ciphertext, and distributing erasure-coded shards across the configured Tank storage nodes." },
  { question: "Can Tank recover a lost recovery key?", answer: "No. The browser does not send your recovery key to storage. If you lose it, Tank cannot decrypt the file or its original filename. Keep the key private and backed up." },
  { question: "Is the file stored on-chain?", answer: "No. Storage nodes hold file shards. The registry stores a commitment to the file manifest and its size, so file contents and recovery keys stay off-chain." },
  { question: "Is Tank ready for important files?", answer: "The current release is a local MVP and testnet pilot. Keep an independent copy: hosted durability, production availability, and long-term retention are not guaranteed." },
];

export default function Home() {
  return (
    <main className="site-shell">
      <a className="skip-link" href="#main-content">Skip to main content</a>
      <Navbar />
      <div id="main-content">
        <Hero />
        <section className="proof-strip" aria-label="Tank architecture summary">
          <div><strong>4 + 2</strong><span>data and parity shards</span></div>
          <div><strong>3 + 1</strong><span>active nodes and spare pilot design</span></div>
          <div><strong>Any 4</strong><span>valid shards reconstruct</span></div>
          <div><strong>Arc</strong><span>public Testnet commitment registry</span></div>
        </section>
        <FeatureSection />

        <section className="technology-section section-block" id="technology" aria-labelledby="technology-title">
          <Reveal className="technology-copy">
            <span className="eyebrow">BUILT WITH REAL TECHNOLOGY</span>
            <h2 id="technology-title">A small stack you can inspect end to end.</h2>
            <p>Tank is open source. The services, SDKs, browser encryption envelope, registry contract, and recovery tests live in one repository.</p>
            <a className="text-link" href={repo} target="_blank" rel="noreferrer">Explore the source <ArrowUpRight aria-hidden="true" size={15} /></a>
          </Reveal>
          <div className="stack-grid">
            {stack.map(({ name, role, icon: Icon }) => <article className="stack-card" key={name}><Icon aria-hidden="true" size={22} strokeWidth={1.5} /><div><h3>{name}</h3><p>{role}</p></div></article>)}
          </div>
          <div className="code-panel">
            <div className="code-heading"><span>TypeScript SDK</span><span className="small-label">LOCAL DEVELOPMENT</span></div>
            <pre><code>{`const manifest = await tank.tank(bytes)

const recovered = await tank.retrieve(
  manifest.file_id
)

const status = await tank.registrationStatus(
  manifest.file_id
)`}</code></pre>
            <div className="code-status"><ShieldCheck aria-hidden="true" size={15} /> Retrieved bytes are integrity checked by the SDK.</div>
          </div>
        </section>

        <section className="roadmap-section section-block" id="roadmap" aria-labelledby="roadmap-title">
          <div className="section-heading"><div><span className="eyebrow">DELIVERY PATH</span><h2 id="roadmap-title">Where Tank stands today.</h2></div><p>Evidence is separated from plans so you can evaluate the system without production promises.</p></div>
          <div className="stage-grid">
            {stages.map(({ label, title, body, items, icon: Icon }, index) => <article className="stage-card" key={title}>
              <div className="stage-card-top"><Icon aria-hidden="true" size={20} /><span>0{index + 1}</span></div>
              <span className={`stage-label stage-${index}`}>{label}</span><h3>{title}</h3><p>{body}</p>
              <ul>{items.map((item) => <li key={item}>{item}</li>)}</ul>
            </article>)}
          </div>
        </section>

        <SessionGate><Workspace /></SessionGate>

        <section className="resources-section section-block" id="resources" aria-labelledby="resources-title">
          <div><span className="eyebrow">RESOURCES</span><h2 id="resources-title">Read the evidence.<br />Run it yourself.</h2><p>Start locally, inspect the code, or review the Arc Testnet acceptance record.</p></div>
          <div className="resource-links">
            <a href={repo} target="_blank" rel="noreferrer"><GitBranch aria-hidden="true" /><span><strong>Source repository</strong><small>Services, SDKs, contract, and tests</small></span><ArrowUpRight aria-hidden="true" /></a>
            <a href={`${repo}/blob/main/tank/README.md`} target="_blank" rel="noreferrer"><Code2 aria-hidden="true" /><span><strong>Developer guide</strong><small>Run the local workflow</small></span><ArrowUpRight aria-hidden="true" /></a>
            <a href={`${repo}/blob/main/tank/docs/arc-testnet-acceptance.md`} target="_blank" rel="noreferrer"><ShieldCheck aria-hidden="true" /><span><strong>Testnet acceptance</strong><small>Public deployment and registration evidence</small></span><ArrowUpRight aria-hidden="true" /></a>
          </div>
        </section>

        <section className="faq-section section-block" aria-labelledby="faq-title">
          <div><span className="eyebrow">BEFORE YOU TANK IT</span><h2 id="faq-title">Clear answers for the current release.</h2></div>
          <div className="faq-list">{questions.map((item) => <details key={item.question}><summary>{item.question}</summary><p>{item.answer}</p></details>)}</div>
        </section>

        <section className="closing-cta"><span className="eyebrow">ENCRYPT · DISTRIBUTE · VERIFY</span><h2>Your next file starts here.</h2><p>Use the local workspace, keep your recovery key, and keep an independent copy.</p><a className="button primary" href="#workspace">Open workspace <ArrowUpRight aria-hidden="true" size={16} /></a></section>
      </div>

      <footer className="footer"><Logo monochrome /><p>Encrypted, verifiable file storage. Built in the open.</p><nav aria-label="Footer navigation"><a href="#how-it-works">How it works</a><a href="#roadmap">Roadmap</a><a href={repo}>GitHub</a></nav><span>Local MVP · Arc Testnet</span></footer>
    </main>
  );
}
