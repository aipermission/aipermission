import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "vite";
import { productionSourceBoundary } from "./scripts/production-source-boundary.mjs";
import { monacoSanitizer } from "./scripts/monaco-sanitizer.mjs";
import { releaseNotesBoundary } from "./scripts/initial-js-budget.mjs";

export default defineConfig({
  plugins: [productionSourceBoundary(), monacoSanitizer(), releaseNotesBoundary(), react(), tailwindcss()],
  server: {
    host: "127.0.0.1",
    port: 3213,
    strictPort: true,
  },
  build: {
    manifest: true,
    chunkSizeWarningLimit: 2500,
    rollupOptions: {
      output: {
        manualChunks: {
          react: ["react", "react-dom", "react-router"],
          terminal: ["@xterm/xterm", "@xterm/addon-fit"],
          ui: ["lucide-react", "@radix-ui/react-slot"],
        },
      },
    },
  },
});
