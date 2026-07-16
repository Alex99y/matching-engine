import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Use import.meta.url so we avoid a node:path/node:url devDependency.
const sdkSrc = new URL("../ts-sdk/src/index.ts", import.meta.url).pathname;

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      // Point directly at the SDK source so no pre-build step is required.
      // Vite resolves .js → .ts imports automatically in bundler mode.
      "ts-sdk": sdkSrc,
    },
  },
});
