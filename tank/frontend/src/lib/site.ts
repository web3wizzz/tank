export const SITE_TITLE = "Tank — Verifiable file storage for Web3";

export const SITE_DESCRIPTION =
  "Encrypt files in your browser, distribute erasure-coded shards, and retrieve verified bytes with Tank's open-source local MVP and Arc Testnet registry.";

const configuredURL = process.env.TANK_SITE_URL;
export const siteURL = configuredURL ? new URL(configuredURL) : undefined;

if (
  siteURL &&
  (
    !["http:", "https:"].includes(siteURL.protocol) ||
    siteURL.username || siteURL.password ||
    siteURL.search || siteURL.hash ||
    siteURL.pathname !== "/"
  )
) {
  throw new Error("TANK_SITE_URL must be the root URL of your website.");
}

export const allowIndexing =
  process.env.NODE_ENV === "production" &&
  process.env.TANK_ALLOW_INDEXING === "true" &&
  siteURL !== undefined;
