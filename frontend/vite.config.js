import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: 3210,
  },
  build: {
    chunkSizeWarningLimit: 2500,
    rollupOptions: {
      output: {
        manualChunks: {
          react: ["react", "react-dom", "react-router-dom"],
          terminal: ["@xterm/xterm", "@xterm/addon-fit"],
          ui: ["lucide-react", "@radix-ui/react-slot"],
        },
      },
    },
  },
});
