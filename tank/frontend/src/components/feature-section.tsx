import { Blocks, FileLock2, Fingerprint, RefreshCw, type LucideIcon } from "lucide-react";
import Reveal from "@/components/reveal";

type Feature = {
  number: string;
  id: string;
  title: string;
  description: string;
  detail: string;
  icon: LucideIcon;
};

const features: Feature[] = [
  {
    number: "01", id: "encrypt", title: "Encrypt in your browser", icon: FileLock2,
    description: "File contents and the original filename are encrypted before upload. The recovery key stays in your hands.",
    detail: "AES-GCM · client-side",
  },
  {
    number: "02", id: "distribute", title: "Distribute file shards", icon: Blocks,
    description: "Each segment becomes four data shards and two parity shards. Initial placement spreads two shards to each of three active nodes.",
    detail: "4 + 2 Reed-Solomon",
  },
  {
    number: "03", id: "verify", title: "Verify integrity", icon: Fingerprint,
    description: "Hash and Merkle checks reject damaged data. A registration worker can record the manifest commitment and size on Arc Testnet.",
    detail: "SHA-256 · Merkle proofs",
  },
  {
    number: "04", id: "retrieve", title: "Retrieve and recover", icon: RefreshCw,
    description: "Any four valid shards can reconstruct a segment. Retrieved ciphertext is verified before local decryption and download.",
    detail: "Verified reconstruction",
  },
];

function FeatureDiagram({ index }: { index: number }) {
  if (index === 0) return <div className="mini-diagram encrypt-diagram"><span>FILE</span><i /><span>CIPHER</span></div>;
  if (index === 1) return <div className="mini-diagram shard-diagram">{Array.from({ length: 6 }, (_, i) => <i className={i > 3 ? "parity" : ""} key={i} />)}</div>;
  if (index === 2) return <div className="mini-diagram verify-diagram"><i /><i /><i /><span>✓</span></div>;
  return <div className="mini-diagram recovery-diagram"><i /><i /><i /><i /><span>FILE</span></div>;
}

export default function FeatureSection() {
  return (
    <section className="feature-section section-block" id="how-it-works" aria-labelledby="feature-title">
      <div className="feature-layout">
        <aside className="feature-intro">
          <span className="eyebrow">HOW IT WORKS</span>
          <h2 id="feature-title">Designed around the full file lifecycle.</h2>
          <p>Encryption, distribution, verification, and recovery form one inspectable workflow.</p>
          <nav className="feature-nav" aria-label="File lifecycle">
            {features.map((feature) => <a href={`#${feature.id}`} key={feature.id}><span>{feature.number}</span>{feature.title}</a>)}
          </nav>
        </aside>
        <div className="feature-cards">
          {features.map((feature, index) => {
            const Icon = feature.icon;
            return (
              <Reveal key={feature.id}>
                <article className="feature-card" id={feature.id}>
                  <div className="feature-card-top">
                    <span className="feature-number">{feature.number}</span>
                    <Icon aria-hidden="true" size={20} strokeWidth={1.6} />
                  </div>
                  <FeatureDiagram index={index} />
                  <h3>{feature.title}</h3>
                  <p>{feature.description}</p>
                  <span className="feature-detail">{feature.detail}</span>
                </article>
              </Reveal>
            );
          })}
        </div>
      </div>
    </section>
  );
}
