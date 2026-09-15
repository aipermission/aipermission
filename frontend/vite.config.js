import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "vite";
import { productionSourceBoundary } from "./scripts/production-source-boundary.mjs";

export default defineConfig({
  plugins: [productionSourceBoundary(), react(), tailwindcss()],
  server: {
    port: 3210,
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
