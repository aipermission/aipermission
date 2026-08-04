import { createDatabaseConnectorModel } from "../_shared/database-connector-model";

const kind = "postgres";
const defaultPort = 5432;
const defaultDatabase = "postgres";

const model = createDatabaseConnectorModel({
  kind,
  label: "Postgres",
  defaultRiskLabel: "read-only",
  includeEmptyPassword: true,
  targetDefaults: {
    name: "main-db",
    connection_mode: "direct",
    host: "127.0.0.1",
    port: defaultPort,
    database: defaultDatabase,
    ssl_mode: "require",
    transport_target_ref: "",
  },
  credentialDefaults: {
    target_id: "",
    profile_label: "readonly",
    username: "",
    password: "",
    risk_label: "read-only",
    managed_by_aipermission: false,
  },
  targetForm: (target) => ({
    connection_mode: target.config?.connection_mode || "direct",
    host: target.config?.host || "",
    port: target.config?.port || defaultPort,
    database: target.config?.database || "",
    ssl_mode: target.config?.ssl_mode || "require",
    transport_target_ref: target.config?.transport_target_ref || "",
  }),
  targetConfig: (form) => ({
    connection_mode: form.connection_mode || "direct",
    host: form.host || "127.0.0.1",
    port: Number(form.port) || defaultPort,
    database: form.database || defaultDatabase,
    ssl_mode: form.ssl_mode || "require",
    transport_target_ref: form.connection_mode === "over_ssh" ? form.transport_target_ref || "" : "",
  }),
  targetEndpoint: ({ target }) => {
    const host = target.config?.host || "host";
    const port = target.config?.port || defaultPort;
    const database = target.config?.database || "database";
    const mode = target.config?.connection_mode === "over_ssh" ? "over ssh" : "direct";
    return `${host}:${port}/${database} · ${mode}`;
  },
  credentialExtras: (row) => ({ managed_by_aipermission: Boolean(row.profile?.public?.managed_by_aipermission) }),
  credentialMetadata: (profile) => {
    const items = [];
    if (profile.public?.username) items.push(`username: ${profile.public.username}`);
    if (profile.public?.managed_by_aipermission) items.push("managed DB role");
    if (profile.risk_label) items.push(`risk: ${profile.risk_label}`);
    if (items.length === 0) items.push("No public metadata");
    return items;
  },
});

export const {
  emptyForm,
  formFromTarget,
  activeCredential,
  syncForm,
  submitDisabled,
  submitLabel,
  save,
  deleteTarget,
  emptyCredentialState,
  credentialStateFromRow,
  credentialFormProps,
  saveCredential,
  deleteCredential,
  credentialRows,
  test,
  canEdit,
  canDelete,
  credentialHint,
  targetEndpoint,
  targetDisplayName,
  targetSubtitle,
  targetProfileLabel,
  usesLiveConsole,
  recoverableRunningActions,
  deleteDialog,
  operationFromError,
} = model;
