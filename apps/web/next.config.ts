import type { NextConfig } from "next";
import createNextIntlPlugin from "next-intl/plugin";

const withNextIntl = createNextIntlPlugin("./src/i18n/request.ts");

const nextConfig: NextConfig = {
  // Self-contained server bundle for the container image (apps/web/Dockerfile).
  output: "standalone",
  // The app is not an npm workspace member: trace from this directory, not the repo root.
  outputFileTracingRoot: import.meta.dirname,
  poweredByHeader: false,
};

export default withNextIntl(nextConfig);
