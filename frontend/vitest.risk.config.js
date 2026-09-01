import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [react()],
  test: {
    environment: "jsdom",
    include: [
      "src/pages/unlock.component.test.jsx",
      "src/components/app-shell-transfer-wiring.component.test.jsx",
      "src/pages/remote-restore-panel.component.test.jsx",
      "src/components/transfer-center.component.test.jsx",
      "src/components/file-transfer/file-transfer-actions.component.test.jsx",
      "src/components/file-transfer/file-transfer-list-state.component.test.jsx",
      "src/components/settings/maintenance-console-panel.component.test.jsx",
      "src/components/vault/vault-action-approval-dialog.component.test.jsx",
      "src/components/file-transfer/file-transfer-confirm-dialogs.component.test.jsx",
      "src/connectors/templates/_shared/network-transport-fields.component.test.jsx",
      "src/connectors/templates/{docker,kafka,kubernetes,mail,rabbitmq,redis,s3}/form.component.test.jsx",
    ],
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
        statements: 55,
        branches: 50,
        functions: 50,
        lines: 55,
      },
    },
  },
});
