import type { Metadata, Viewport } from "next";
import { Inter, Space_Grotesk } from "next/font/google";
import {
  SITE_TITLE,
  SITE_DESCRIPTION,
  siteURL,
  allowIndexing,
} from "@/lib/site";
import "./globals.css";

const inter = Inter({
  subsets: ["latin"],
  variable: "--font-body",
  display: "swap",
});

const space = Space_Grotesk({
  subsets: ["latin"],
  variable: "--font-heading",
  display: "swap",
});

export const metadata: Metadata = {
  metadataBase: siteURL ?? new URL("http://localhost:3000"),
  title: {
    default: SITE_TITLE,
    template: "%s | Tank",
  },
  description: SITE_DESCRIPTION,
  applicationName: "Tank",
  ...(siteURL ? { alternates: { canonical: siteURL.href } } : {}),
  robots: {
    index: allowIndexing,
    follow: allowIndexing,
  },
  openGraph: {
    type: "website",
    siteName: "Tank",
    title: SITE_TITLE,
    description: SITE_DESCRIPTION,
    locale: "en_US",
    ...(siteURL ? { url: siteURL.href } : {}),
  },
  twitter: {
    card: "summary_large_image",
    title: SITE_TITLE,
    description: SITE_DESCRIPTION,
    images: ["/opengraph-image"],
  },
};

export const viewport: Viewport = {
  themeColor: "#07090b",
};

export default function RootLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="en" className={`${inter.variable} ${space.variable}`}>
      <body>{children}</body>
    </html>
  );
}
