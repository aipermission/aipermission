import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

import { listBehaviorOwners } from "./scripts/coverage-owner-policy.mjs";

export default defineConfig({
  plugins: [react()],
  test: {
    environment: "jsdom",
    include: ["src/**/*.component.test.{js,jsx}"],
    // The ratchet needs reproducible instrumentation and execution order.
    // Regular test suites remain parallel and continue to use V8 coverage.
    fileParallelism: false,
    maxWorkers: 1,
    setupFiles: ["./src/test/setup.js"],
    restoreMocks: true,
    unstubGlobals: true,
    coverage: {
      provider: "istanbul",
      all: true,
      include: listBehaviorOwners(process.cwd()),
      reporter: ["text-summary", "json-summary"],
    },
  },
});
