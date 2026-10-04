export const SITE_TITLE = "Tank — Verifiable file storage for Web3";

export const SITE_DESCRIPTION =
  "Store files across nodes, recover missing shards, and retrieve verified bytes. Explore Tank's local storage demo and blockchain registration.";

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
