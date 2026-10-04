import type { MetadataRoute } from "next";
import { allowIndexing, siteURL } from "@/lib/site";

export default function robots(): MetadataRoute.Robots {
  if (!allowIndexing || !siteURL) {
    return {
      rules: { userAgent: "*", disallow: "/" },
    };
  }

  return {
    rules: {
      userAgent: "*",
      allow: "/",
      disallow: "/api/",
    },
    sitemap: new URL("/sitemap.xml", siteURL).href,
  };
}
