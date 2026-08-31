import { connectorCredentialRows, createTargetProfileLifecycle } from "../_shared/target-profile-lifecycle";
import { credentialPayload, targetEndpoint as brokerEndpoint } from "./model-helpers";

export { credentialPayload } from "./model-helpers";

const defaultRiskLabel = "stream read";
const lifecycle = createTargetProfileLifecycle({
  connectorKind: "kafka",
  connectorLabel: "Kafka",
  targetPayload: (form) => ({ name: form.name, config: targetConfig(form) }),
  profilePayload: (form, { profile }) => credentialPayload(form, profile?.kind),
});

export const { credentialFormProps, deleteCredential, deleteTarget, save, saveCredential, test } = lifecycle;

export function emptyForm() {
  return {
    connector_kind: "kafka",
    name: "event-stream",
    server_family: "kafka",
    connection_mode: "direct",
    bootstrap_brokers: "127.0.0.1:9092",
    transport_target_ref: "",
    tls_enabled: false,
    allow_insecure_plain_sasl: false,
    tls_server_name: "",
    tls_ca_pem: "",
    profile_label: "monitor",
    sasl_mechanism: "none",
    existing_sasl_mechanism: "none",
    username: "",
    password: "",
    risk_label: defaultRiskLabel,
  };
}

export function formFromTarget({ target, profile }) {
  const selectedProfile = profile || (target?.profiles?.length === 1 ? target.profiles[0] : {});
  return {
    connector_kind: "kafka",
    profile_id: selectedProfile.id ? String(selectedProfile.id) : "",
    name: target.name || "",
    server_family: target.config?.server_family || "kafka",
    connection_mode: target.config?.connection_mode || "direct",
    bootstrap_brokers: target.config?.bootstrap_brokers || "127.0.0.1:9092",
    transport_target_ref: target.config?.transport_target_ref || "",
    tls_enabled: Boolean(target.config?.tls_enabled),
    allow_insecure_plain_sasl: Boolean(target.config?.allow_insecure_plain_sasl),
    tls_server_name: target.config?.tls_server_name || "",
    tls_ca_pem: target.config?.tls_ca_pem || "",
    profile_label: selectedProfile.label || "monitor",
    sasl_mechanism: selectedProfile.public?.mechanism || "none",
    existing_sasl_mechanism: selectedProfile.public?.mechanism || "none",
    username: selectedProfile.public?.username || "",
    password: "",
    risk_label: selectedProfile.risk_label || defaultRiskLabel,
  };
}

export function activeCredential() {
  return null;
}

export function syncForm({ form }) {
  if (form.connector_kind !== "kafka") return form;
  const next = { ...form };
  if (next.connection_mode === "direct") next.transport_target_ref = "";
  if (!next.tls_enabled) {
    next.tls_server_name = "";
    next.tls_ca_pem = "";
  }
  if (next.sasl_mechanism === "none") {
    next.username = "";
    next.password = "";
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
  const firstTarget = targets.find((target) => target.connector_kind === "kafka");
  return {
    form: {
      target_id: String(firstTarget?.id || ""),
      profile_label: "monitor",
      sasl_mechanism: "none",
      existing_sasl_mechanism: "none",
      username: "",
      password: "",
      risk_label: defaultRiskLabel,
    },
  };
}
export function credentialStateFromRow({ row }) {
  return {
    form: {
      target_id: String(row.target_id || ""),
      profile_label: row.name,
      sasl_mechanism: row.profile?.public?.mechanism || "none",
      existing_sasl_mechanism: row.profile?.public?.mechanism || "none",
      username: row.profile?.public?.username || "",
      password: "",
      risk_label: row.profile?.risk_label || defaultRiskLabel,
    },
  };
}
export function credentialRows({ targets }) {
  return connectorCredentialRows({
    targets,
    connectorKind: "kafka",
    connectorLabel: "Kafka / Redpanda",
    targetEndpoint,
    credentialMetadata,
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
  return targetEndpointValue(target);
}
function targetEndpointValue(target) {
  const brokers = brokerEndpoint(target);
  const mode = target.config?.connection_mode === "over_ssh" ? "over ssh" : "direct";
  return `${brokers} · ${mode}`;
}
export function targetDisplayName({ target }) {
  return target?.target_name || target?.name || "Kafka target";
}
export function targetSubtitle({ target }) {
  return targetEndpoint({ target });
}
export function targetProfileLabel({ target }) {
  return target?.profile_label || "monitor";
}
export function usesLiveConsole() {
  return false;
}
export function recoverableRunningActions() {
  return [];
}
export function deleteDialog({ target }) {
  return {
    title: target ? `Delete ${target.name}` : "Delete connector",
    description: "Remove this Kafka / Redpanda target, credential profiles, and token action permissions from aipermission.",
    details: [
      { label: "Connector", value: target?.name },
      { label: "Reference", value: target ? `${target.connector_kind}:${target.id}` : "" },
    ],
    notice: "This does not change the Kafka or Redpanda cluster.",
    actions: [
      { label: "Cancel", action: "close", variant: "outline" },
      { label: "Delete connector", pendingLabel: "Deleting...", removeKey: false },
    ],
  };
}
export function operationFromError() {
  return null;
}

function targetConfig(form) {
  return {
    server_family: form.server_family === "redpanda" ? "redpanda" : "kafka",
    connection_mode: form.connection_mode || "direct",
    bootstrap_brokers: form.bootstrap_brokers || "127.0.0.1:9092",
    transport_target_ref: form.connection_mode === "over_ssh" ? form.transport_target_ref || "" : "",
    tls_enabled: Boolean(form.tls_enabled),
    allow_insecure_plain_sasl: Boolean(form.allow_insecure_plain_sasl),
    tls_server_name: form.tls_enabled ? form.tls_server_name || "" : "",
    tls_ca_pem: form.tls_enabled ? form.tls_ca_pem || "" : "",
  };
}
function credentialMetadata(profile) {
  const mechanism = profile.public?.mechanism || "none";
  const items = [`SASL: ${mechanism}`];
  if (profile.public?.username) items.push(`username: ${profile.public.username}`);
  if (profile.risk_label) items.push(`risk: ${profile.risk_label}`);
  return items;
}
