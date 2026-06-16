/** @type {import('next').NextConfig} */
const nextConfig = {
  reactStrictMode: true,
  // UI Sprint 1 is mock-only: there is no backend to proxy to yet. When the
  // management OpenAPI backend lands, add rewrites()/env wiring here.
};

export default nextConfig;
