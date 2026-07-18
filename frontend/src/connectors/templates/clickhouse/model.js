import { createDatabaseConnectorModel } from "../_shared/database-connector-model";

const kind = "clickhouse";
const defaultPort = 9000;
const defaultDatabase = "default";

const model = createDatabaseConnectorModel({
  kind,
  label: "ClickHouse",
  defaultRiskLabel: "read-only analytics",
  targetDefaults: {
    name: "analytics-db",
    connection_mode: "direct",
    host: "127.0.0.1",
    port: defaultPort,
    database: defaultDatabase,
    tls_mode: "disable",
    transport_target_ref: "",
  },
  credentialDefaults: { target_id: "", profile_label: "readonly", username: "", password: "", risk_label: "read-only analytics" },
  targetForm: (target) => ({
    connection_mode: target.config?.connection_mode || "direct",
    host: target.config?.host || "127.0.0.1",
    port: target.config?.port || defaultPort,
    database: target.config?.database || defaultDatabase,
    tls_mode: target.config?.tls_mode || "disable",
    transport_target_ref: target.config?.transport_target_ref || "",
  }),
  targetConfig: (form) => ({
    connection_mode: form.connection_mode || "direct",
    host: form.host || "127.0.0.1",
    port: Number(form.port) || defaultPort,
    database: form.database || defaultDatabase,
    tls_mode: form.tls_mode || "disable",
    transport_target_ref: form.connection_mode === "over_ssh" ? form.transport_target_ref || "" : "",
  }),
  targetEndpoint: ({ target }) => {
    const host = target.config?.host || "127.0.0.1";
    const port = target.config?.port || defaultPort;
    const database = target.config?.database || defaultDatabase;
    const mode = target.config?.connection_mode === "over_ssh" ? "over ssh" : "direct";
    return `${host}:${port}/${database} · ${mode}`;
  },
});

export const {
  emptyForm, formFromTarget, activeCredential, syncForm, submitDisabled, submitLabel, save, deleteTarget,
  emptyCredentialState, credentialStateFromRow, credentialFormProps, saveCredential, deleteCredential,
  credentialRows, test, canEdit, canDelete, credentialHint, targetEndpoint, targetDisplayName, targetSubtitle,
  targetProfileLabel, usesLiveConsole, recoverableRunningActions, deleteDialog, operationFromError,
} = model;
