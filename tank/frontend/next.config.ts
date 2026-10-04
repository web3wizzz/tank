import { resolve as resolveTankPath } from "node:path";
import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  /* config options here */
};

// Tank: include the linked SDK in the project root
nextConfig.turbopack = {
  ...nextConfig.turbopack,
  root: resolveTankPath(__dirname, ".."),
};

nextConfig.outputFileTracingRoot = resolveTankPath(__dirname, "..");

export default nextConfig;
