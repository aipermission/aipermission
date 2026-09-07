import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

import { listBehaviorOwners } from "./scripts/coverage-owner-policy.mjs";

export default defineConfig({
  plugins: [react()],
  test: {
    environment: "jsdom",
    include: ["src/**/*.component.test.{js,jsx}"],
    // Coverage merges of differently mocked modules are order-sensitive across
    // workers. Keep the ratchet deterministic; regular test suites stay parallel.
    fileParallelism: false,
    maxWorkers: 1,
    setupFiles: ["./src/test/setup.js"],
    restoreMocks: true,
    unstubGlobals: true,
    coverage: {
      provider: "v8",
      include: listBehaviorOwners(process.cwd()),
      reporter: ["text-summary", "json-summary"],
      reportsDirectory: "coverage/changed",
    },
  },
});
