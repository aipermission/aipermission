import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [react()],
  test: {
    environment: "jsdom",
    include: ["src/**/*.component.test.{js,jsx}"],
    setupFiles: ["./src/test/setup.js"],
    restoreMocks: true,
    unstubGlobals: true,
    coverage: {
      provider: "v8",
      include: [
        "src/lib/use-connector-permissions.js",
        "src/components/console/connector-action-approval-dialog.jsx",
        "src/components/console/use-console-page-state.js",
      ],
      reporter: ["text"],
      thresholds: {
        perFile: true,
        statements: 65,
        branches: 20,
        functions: 50,
        lines: 65,
      },
    },
  },
});
