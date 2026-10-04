import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [react()],
  test: {
    environment: "jsdom",
    include: ["src/**/*.component.test.{js,jsx,ts,tsx}"],
    setupFiles: ["./src/test/setup.ts"],
    restoreMocks: true,
    unstubGlobals: true,
    coverage: {
      provider: "v8",
      include: [
        "src/lib/use-connector-permissions.ts",
        "src/connectors/templates/_shared/action-runner.ts",
        "src/connectors/templates/_shared/target-profile-lifecycle.ts",
        "src/connectors/profile-lifecycle/persistence.ts",
        "src/connectors/profile-lifecycle/atomic-save.ts",
        "src/components/console/connector-action-approval-dialog.tsx",
        "src/components/console/connector-token-permission-panel.tsx",
        "src/components/console/use-console-page-state.ts",
      ],
      reporter: ["text"],
      thresholds: {
        perFile: true,
        statements: 75,
        branches: 60,
        functions: 70,
        lines: 75,
      },
    },
  },
});
