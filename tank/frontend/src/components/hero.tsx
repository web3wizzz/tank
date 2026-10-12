import { ArrowRight, BookOpen } from "lucide-react";
import Reveal from "@/components/reveal";

function NetworkGraphic() {
  return (
    <div className="network-graphic" aria-label="Encrypted file split into six shards across three storage nodes">
      <svg viewBox="0 0 740 300" role="img" aria-labelledby="network-title network-description">
        <title id="network-title">Tank file distribution network</title>
        <desc id="network-description">An encrypted file becomes four data shards and two parity shards distributed across three nodes.</desc>
        <defs>
          <radialGradient id="node-glow">
            <stop offset="0" stopColor="#00E5FF" stopOpacity=".22" />
            <stop offset="1" stopColor="#00E5FF" stopOpacity="0" />
          </radialGradient>
        </defs>
        <circle cx="370" cy="150" r="116" fill="url(#node-glow)" />
        <g className="network-lines">
          <path d="M135 82 334 132M135 218l199-50M406 132l199-50M406 168l199 50M135 82l470 136M135 218 605 82" />
        </g>
        <g className="network-file">
          <rect x="334" y="112" width="72" height="76" rx="10" />
          <path d="M354 132h32M354 147h32M354 162h20" />
        </g>
        {[
          [108, 56, "NODE 01"], [108, 192, "NODE 02"], [578, 56, "NODE 03"], [578, 192, "SPARE"],
        ].map(([x, y, label]) => (
          <g className="network-node" key={String(label)} transform={`translate(${x} ${y})`}>
            <rect width="54" height="54" rx="9" />
            <path d="M15 18h24M15 27h24M15 36h24" />
            <text x="27" y="72" textAnchor="middle">{label}</text>
          </g>
        ))}
        {[[-1, 0], [0, 0], [1, 0], [-1, 1], [0, 1], [1, 1]].map(([column, row], index) => (
          <rect
            className={index > 3 ? "parity-packet" : "data-packet"}
            key={index}
            x={357 + Number(column) * 18}
            y={218 + Number(row) * 19}
            width="12"
            height="12"
            rx="2"
          />
        ))}
        <text className="network-label" x="370" y="282" textAnchor="middle">4 DATA + 2 PARITY</text>
      </svg>
      <div className="network-legend" aria-hidden="true">
        <span><i className="legend-data" /> Data shard</span>
        <span><i className="legend-parity" /> Parity shard</span>
        <span><i className="legend-line" /> Encrypted distribution</span>
      </div>
    </div>
  );
}

export default function Hero() {
  return (
    <section className="hero" aria-labelledby="hero-title">
      <div className="hero-grid" aria-hidden="true" />
      <Reveal className="hero-copy">
        <span className="eyebrow"><span className="status-dot" /> ENCRYPTED · DISTRIBUTED · VERIFIABLE</span>
        <h1 id="hero-title"><span>TANK YOUR FILES.</span><span>RETRIEVE WITH CONFIDENCE.</span></h1>
        <p>
          Tank encrypts files in your browser, distributes erasure-coded shards across storage nodes,
          and verifies integrity when you retrieve. Manifest commitments can be registered on Arc Testnet.
        </p>
        <div className="hero-actions">
          <a className="button primary" href="#workspace">Tank a file <ArrowRight aria-hidden="true" size={17} /></a>
          <a className="button secondary" href="https://github.com/web3wizzz/tank/blob/main/tank/README.md" target="_blank" rel="noreferrer">
            Developer docs <BookOpen aria-hidden="true" size={16} />
          </a>
        </div>
        <p className="hero-caveat">Local MVP available now. Keep an independent copy while the hosted pilot is built.</p>
      </Reveal>
      <Reveal className="hero-visual" delay={0.08}><NetworkGraphic /></Reveal>
    </section>
  );
}
