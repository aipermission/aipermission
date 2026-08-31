import { connectorCredentialRows, createTargetProfileLifecycle } from "../_shared/target-profile-lifecycle";

const emptyRedisCredentialForm = { target_id: "", profile_label: "default", username: "", password: "", risk_label: "cache access" };
const defaultServerFamily = "redis";
export const connectorProductLabel = "Redis / Valkey";
const lifecycle = createTargetProfileLifecycle({
  connectorKind: "redis",
  connectorLabel: connectorProductLabel,
  targetPayload: (form) => ({ name: form.name, config: redisTargetConfigFromForm(form) }),
  profilePayload: redisProfilePayloadFromForm,
  credentialCreatedMessage: ({ target }) => `${serverProductLabel(target)} credential created.`,
  credentialUpdatedMessage: ({ target }) => `${serverProductLabel(target)} credential updated.`,
});

export const { credentialFormProps, deleteCredential, deleteTarget, save, saveCredential, test } = lifecycle;

export function emptyForm() {
  return {
    connector_kind: "redis",
    name: "redis-cache",
    server_family: defaultServerFamily,
    connection_mode: "direct",
    host: "127.0.0.1",
    port: 6379,
    database: 0,
    tls_mode: "auto",
    transport_target_ref: "",
    profile_label: "default",
    username: "",
    password: "",
    risk_label: "cache access",
  };
}

export function formFromTarget({ target, profile }) {
  const selectedProfile = profile || (target?.profiles?.length === 1 ? target.profiles[0] : {});
  return {
    connector_kind: "redis",
    profile_id: selectedProfile.id ? String(selectedProfile.id) : "",
    name: target.name || "",
    server_family: target.config?.server_family || defaultServerFamily,
    connection_mode: target.config?.connection_mode || "direct",
    host: target.config?.host || "127.0.0.1",
    port: target.config?.port || 6379,
    database: target.config?.database || 0,
    tls_mode: target.config?.tls_mode || "disable",
    transport_target_ref: target.config?.transport_target_ref || "",
    profile_label: selectedProfile.label || "default",
    username: selectedProfile.public?.username || "",
    password: "",
    risk_label: selectedProfile.risk_label || "cache access",
  };
}

export function activeCredential() {
  return null;
}

export function syncForm({ form }) {
  if (form.connector_kind !== "redis") return form;
  const next = { ...form };
  if (next.connection_mode === "direct") {
    next.transport_target_ref = "";
  }
  return next;
}

export function submitDisabled({ state }) {
  return state.state === "saving";
}

export function submitLabel({ state, mode }) {
  if (state.state === "saving") return "Saving...";
  return mode === "edit" ? "Save changes" : "Create connector";
}

export function emptyCredentialState({ targets = [] } = {}) {
  const firstTarget = targets.find((target) => target.connector_kind === "redis");
  return {
    form: {
      ...emptyRedisCredentialForm,
      target_id: String(firstTarget?.id || ""),
    },
  };
}

export function credentialStateFromRow({ row }) {
  return {
    form: {
      target_id: String(row.target_id || ""),
      profile_label: row.name,
      username: row.profile?.public?.username || "",
      password: "",
      risk_label: row.profile?.risk_label || "",
    },
  };
}

export function credentialRows({ targets }) {
  return connectorCredentialRows({
    targets,
    connectorKind: "redis",
    connectorLabel: serverProductLabel,
    targetEndpoint,
    credentialMetadata,
    includeTarget: true,
  });
}

export function canEdit() {
  return true;
}

export function canDelete() {
  return true;
}

export function credentialHint() {
  return null;
}

export function targetEndpoint({ target }) {
  const host = target.config?.host || "127.0.0.1";
  const port = target.config?.port || 6379;
  const database = target.config?.database || 0;
  const mode = target.config?.connection_mode === "over_ssh" ? "over ssh" : "direct";
  return `${host}:${port}/${database} · ${mode}`;
}

export function targetDisplayName({ target }) {
  if (!target) return `${connectorProductLabel} target`;
  return target.target_name || target.name || `${serverProductLabel(target)} target`;
}

export function targetSubtitle({ target }) {
  return `${serverProductLabel(target)} · ${targetEndpoint({ target })}`;
}

export function targetProfileLabel({ target }) {
  return target?.profile_label || "default";
}

export function usesLiveConsole() {
  return false;
}

export function recoverableRunningActions() {
  return [];
}

export function deleteDialog({ target }) {
  const product = serverProductLabel(target);
  return {
    title: target ? `Delete ${target.name}` : "Delete connector",
    description: `Remove this ${product} connector target, credential profiles, and token action permissions from aipermission.`,
    details: [
      { label: "Connector", value: target?.name },
      { label: "Reference", value: target ? `${target.connector_kind}:${target.id}` : "" },
    ],
    notice: `This removes the connector target and its credential profiles. It does not change the ${product} server.`,
    actions: [
      { label: "Cancel", action: "close", variant: "outline" },
      { label: "Delete connector", pendingLabel: "Deleting...", removeKey: false },
    ],
  };
}

export function operationFromError() {
  return null;
}

function redisTargetConfigFromForm(form) {
  const tlsMode = ["auto", "verify_full"].includes(form.tls_mode) ? form.tls_mode : "disable";
  return {
    server_family: form.server_family === "valkey" ? "valkey" : defaultServerFamily,
    connection_mode: form.connection_mode || "direct",
    host: form.host || "127.0.0.1",
    port: Number(form.port) || 6379,
    database: Number(form.database) || 0,
    tls_mode: tlsMode,
    transport_target_ref: form.connection_mode === "over_ssh" ? form.transport_target_ref || "" : "",
  };
}

function redisProfilePayloadFromForm(form, { profile, operation }) {
  return {
    kind: profile?.kind || "username_password",
    label: form.profile_label,
    public: { username: form.username },
    ...(form.password ? { secret: { password: form.password } } : {}),
    risk_label: operation.startsWith("target") ? form.risk_label || "cache access" : form.risk_label,
  };
}

export function serverProductLabel(targetOrConfig) {
  const config = targetOrConfig?.config || targetOrConfig || {};
  return config.server_family === "valkey" ? "Valkey" : "Redis";
}

export function validateStringWrite({ key, value }) {
  if (!String(key || "").trim()) return "Key is required.";
  if (!String(value || "").trim()) return "Value is required.";
  return "";
}

function credentialMetadata(profile) {
  const items = [];
  if (profile.public?.username) items.push(`username: ${profile.public.username}`);
  if (profile.risk_label) items.push(`risk: ${profile.risk_label}`);
  if (items.length === 0) items.push("No public metadata");
  return items;
}
