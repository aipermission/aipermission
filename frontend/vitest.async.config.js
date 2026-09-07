import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";
import { asyncStateTestIncludes } from "./test-suite-manifests.mjs";

export default defineConfig({
  plugins: [react()],
  test: {
    environment: "jsdom",
    include: asyncStateTestIncludes,
    setupFiles: ["./src/test/setup.js"],
    restoreMocks: true,
    unstubGlobals: true,
  },
});
