import { resolve as resolveTankPath } from "node:path";
import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Container builds copy only traced production files; developer launches
  // retain the existing next start workflow.
  output: process.env.TANK_STANDALONE_BUILD === "1" ? "standalone" : undefined,
};

// Tank: include the linked SDK in the project root
nextConfig.turbopack = {
  ...nextConfig.turbopack,
  root: resolveTankPath(__dirname, ".."),
};

nextConfig.outputFileTracingRoot = resolveTankPath(__dirname, "..");

export default nextConfig;
