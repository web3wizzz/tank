import type { MetadataRoute } from "next";
import { allowIndexing, siteURL } from "@/lib/site";

export default function sitemap(): MetadataRoute.Sitemap {
  if (!allowIndexing || !siteURL) return [];

  return [{ url: siteURL.href }];
}
