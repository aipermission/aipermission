import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";
import { riskCoverageTestIncludes } from "./test-suite-manifests.mjs";

export default defineConfig({
  plugins: [react()],
  test: {
    environment: "jsdom",
    include: riskCoverageTestIncludes,
    setupFiles: ["./src/test/setup.js"],
    restoreMocks: true,
    unstubGlobals: true,
    coverage: {
      provider: "v8",
      include: [
        "src/pages/unlock.jsx",
        "src/components/transfer-center.jsx",
        "src/components/file-transfer/file-transfer-actions.js",
        "src/components/file-transfer/file-transfer-list-state.js",
        "src/components/settings/maintenance-console-panel.jsx",
        "src/components/vault/vault-action-approval-dialog.jsx",
        "src/components/file-transfer/file-transfer-confirm-dialogs.jsx",
        "src/connectors/templates/_shared/network-transport-fields.jsx",
        "src/connectors/templates/{docker,kafka,kubernetes,mail,rabbitmq,redis,s3}/form.jsx",
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
