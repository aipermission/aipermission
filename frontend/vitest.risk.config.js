import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";
import { riskCoverageTestIncludes } from "./test-suite-manifests.mjs";

export default defineConfig({
  plugins: [react()],
  test: {
    environment: "jsdom",
    include: riskCoverageTestIncludes,
    setupFiles: ["./src/test/setup.ts"],
    restoreMocks: true,
    unstubGlobals: true,
    coverage: {
      provider: "v8",
      include: [
        "src/pages/unlock.jsx",
        "src/components/transfer-center.jsx",
        "src/components/file-transfer/file-transfer-actions.ts",
        "src/components/file-transfer/file-transfer-list-state.ts",
        "src/components/settings/maintenance-console-panel.jsx",
        "src/components/vault/vault-action-approval-dialog.jsx",
        "src/components/file-transfer/file-transfer-confirm-dialogs.tsx",
        "src/connectors/templates/_shared/network-transport-fields.{jsx,tsx}",
        "src/connectors/templates/{docker,kafka,kubernetes,mail,rabbitmq,redis,s3}/form.{jsx,tsx}",
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
