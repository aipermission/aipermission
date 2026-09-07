export const asyncStateOwnerTests = {
  "src/components/console/use-connector-approval-dialog.js": ["src/components/console/use-connector-approval-dialog.component.test.jsx"],
  "src/components/console/use-console-connections.js": ["src/components/console/use-console-connections.component.test.jsx"],
  "src/components/console/use-console-messages.js": ["src/components/console/use-console-messages.component.test.jsx"],
  "src/components/console/use-console-recovery-state.js": ["src/components/console/use-console-recovery-state.component.test.jsx"],
  "src/components/console/use-console-session-coordinator.js": [
    "src/components/console/use-console-session-coordinator.component.test.jsx",
  ],
  "src/components/console/use-console-workspace-session.js": ["src/components/console/use-console-workspace-session.component.test.jsx"],
  "src/components/file-transfer/use-transfer-browser.js": ["src/components/file-transfer/use-transfer-browser.component.test.jsx"],
  "src/components/file-transfer/use-transfer-batch.js": ["src/components/file-transfer/use-transfer-batch.component.test.jsx"],
  "src/components/file-transfer/use-transfer-download.js": ["src/components/file-transfer/use-transfer-download.component.test.jsx"],
  "src/components/file-transfer/use-transfer-queues.js": ["src/components/file-transfer/use-transfer-queues.component.test.jsx"],
  "src/components/settings/maintenance-console-panel.jsx": ["src/components/settings/maintenance-console-panel.component.test.jsx"],
  "src/components/use-gateway-activity-resources.js": ["src/components/use-gateway-resources.component.test.jsx"],
  "src/components/use-gateway-core-resources.js": ["src/components/use-gateway-resources.component.test.jsx"],
  "src/components/vault/use-vault-action-approvals.js": ["src/components/vault/use-vault-action-approvals.component.test.jsx"],
  "src/components/vault/use-vault-bindings.js": ["src/components/vault/use-vault-bindings.component.test.jsx"],
  "src/components/vault/use-vault-collection.js": ["src/components/vault/use-vault-collection.component.test.jsx"],
  "src/components/vault/use-vault-value-actions.js": ["src/components/vault/use-vault-value-actions.component.test.jsx"],
  "src/connectors/editor/use-connector-inventory.js": ["src/connectors/editor/use-connector-inventory.component.test.jsx"],
  "src/connectors/editor/use-connector-connection-tests.js": ["src/connectors/editor/use-connector-connection-tests.component.test.jsx"],
  "src/connectors/templates/_shared/action-runner.js": ["src/connectors/templates/_shared/action-runner.component.test.jsx"],
  "src/connectors/templates/_shared/use-sql-metadata.js": ["src/connectors/templates/_shared/use-sql-console.component.test.jsx"],
  "src/connectors/templates/_shared/use-sql-console.js": ["src/connectors/templates/_shared/use-sql-console.component.test.jsx"],
  "src/connectors/templates/docker/use-docker-browser.js": ["src/connectors/templates/docker/use-docker-browser.component.test.jsx"],
  "src/connectors/templates/kafka/use-kafka-browser.js": ["src/connectors/templates/kafka/use-kafka-browser.component.test.jsx"],
  "src/connectors/templates/kubernetes/use-kubernetes-browser.js": [
    "src/connectors/templates/kubernetes/use-kubernetes-browser.component.test.jsx",
  ],
  "src/connectors/templates/postgres/use-postgres-backup-restore.js": [
    "src/connectors/templates/postgres/use-postgres-backup-restore.component.test.jsx",
  ],
  "src/connectors/templates/postgres/use-postgres-provisioning.js": [
    "src/connectors/templates/postgres/use-postgres-provisioning.component.test.jsx",
  ],
  "src/connectors/templates/rabbitmq/use-rabbitmq-browser.js": [
    "src/connectors/templates/rabbitmq/use-rabbitmq-browser.component.test.jsx",
  ],
  "src/connectors/templates/redis/use-redis-browser.js": ["src/connectors/templates/redis/use-redis-browser.component.test.jsx"],
  "src/connectors/templates/s3/use-s3-browser.js": ["src/connectors/templates/s3/use-s3-browser.component.test.jsx"],
  "src/connectors/templates/ssh/bulk-command-dialog.jsx": ["src/connectors/templates/ssh/bulk-command-dialog.component.test.jsx"],
  "src/lib/request-guard.js": ["src/lib/request-guard.test.js"],
  "src/lib/use-connector-permissions.js": ["src/lib/use-connector-permissions.component.test.jsx"],
  "src/pages/remote-restore-panel.jsx": ["src/pages/remote-restore-panel.component.test.jsx"],
  "src/pages/unlock-create-panel.jsx": ["src/pages/unlock.component.test.jsx"],
  "src/pages/unlock-database-panel.jsx": ["src/pages/unlock.component.test.jsx"],
  "src/pages/unlock-import-panel.jsx": ["src/pages/unlock.component.test.jsx"],
  "src/pages/use-history-page-state.js": ["src/pages/history.component.test.jsx"],
};

export const asyncStateTestIncludes = [...new Set(Object.values(asyncStateOwnerTests).flat())]
  .filter((file) => file.includes(".component.test."))
  .sort();

export const riskCoverageTestIncludes = [
  "src/pages/unlock.component.test.jsx",
  "src/pages/remote-restore-panel.component.test.jsx",
  "src/components/transfer-center.component.test.jsx",
  "src/components/file-transfer/use-transfer-center-state.component.test.jsx",
  "src/components/file-transfer/file-transfer-actions.component.test.jsx",
  "src/components/file-transfer/file-transfer-list-state.component.test.jsx",
  "src/components/settings/maintenance-console-panel.component.test.jsx",
  "src/components/vault/vault-action-approval-dialog.component.test.jsx",
  "src/components/file-transfer/file-transfer-confirm-dialogs.component.test.jsx",
  "src/connectors/templates/_shared/network-transport-fields.component.test.jsx",
  "src/connectors/templates/{docker,kafka,kubernetes,mail,rabbitmq,redis,s3}/form.component.test.jsx",
];
