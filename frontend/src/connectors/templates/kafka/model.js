import { apiDelete, apiPost, apiPut } from "../../../lib/api";
import { createTargetWithProfile, updateTargetWithProfile } from "../target-profile-save";
import { credentialPayload, targetEndpoint as brokerEndpoint } from "./model-helpers";

export { credentialPayload } from "./model-helpers";

const defaultRiskLabel = "stream read";

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

export function activeCredential() { return null; }

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

export function submitDisabled({ state }) { return state.state === "saving"; }
export function submitLabel({ state, mode }) {
  if (state.state === "saving") return "Saving...";
  return mode === "edit" ? "Save changes" : "Create connector";
}
export async function save({ mode, form, target }) {
  if (mode === "edit") return updateTarget({ form, target });
  return createTarget({ form });
}
export async function deleteTarget({ target }) { await apiDelete(`/api/connector-targets/${target.id}`); }

export function emptyCredentialState({ targets = [] } = {}) {
  const firstTarget = targets.find((target) => target.connector_kind === "kafka");
  return { form: { target_id: String(firstTarget?.id || ""), profile_label: "monitor", sasl_mechanism: "none", existing_sasl_mechanism: "none", username: "", password: "", risk_label: defaultRiskLabel } };
}
export function credentialStateFromRow({ row }) {
  return { form: {
    target_id: String(row.target_id || ""),
    profile_label: row.name,
    sasl_mechanism: row.profile?.public?.mechanism || "none",
    existing_sasl_mechanism: row.profile?.public?.mechanism || "none",
    username: row.profile?.public?.username || "",
    password: "",
    risk_label: row.profile?.risk_label || defaultRiskLabel,
  } };
}
export function credentialFormProps({ targets, formState, setFormState, formMode, state, onSubmit }) {
  return {
    form: formState.form, formMode, targets, state,
    onChange: (form) => setFormState({ form }),
    onSubmit: (event) => onSubmit(event, formMode === "edit" ? "update" : "create"),
  };
}
export async function saveCredential({ operation, row, formState }) {
  const form = formState.form;
  const payload = credentialPayload(form, row?.profile?.kind);
  if (operation === "create") {
    await apiPost(`/api/connector-targets/${form.target_id}/profiles`, payload);
    return { message: "Kafka credential created." };
  }
  if (operation === "update") {
    if (!row) throw new Error("Kafka credential is not loaded.");
    await apiPut(`/api/connector-targets/${form.target_id}/profiles/${row.id}`, payload);
    return { message: "Kafka credential updated." };
  }
  throw new Error("Unsupported Kafka credential operation.");
}
export async function deleteCredential({ row }) { await apiDelete(`/api/connector-targets/${row.target_id}/profiles/${row.id}`); }
export function credentialRows({ targets }) {
  return targets.flatMap((target) => (target.profiles || [])
    .filter(() => target.connector_kind === "kafka")
    .map((profile) => ({
      row_id: `${target.connector_kind}:${target.id}:${profile.id}`,
      connector_kind: target.connector_kind,
      resource_kind: "credential_profile",
      connector_label: "Kafka / Redpanda",
      id: profile.id,
      target_id: target.id,
      name: profile.label,
      kind: profile.kind,
      profile,
      target_label: target.name,
      target_detail: targetEndpoint({ target }),
      metadata: credentialMetadata(profile),
      delete_disabled: "",
    })));
}
export async function test({ target, profile }) {
  const selectedProfile = profile || (target?.profiles?.length === 1 ? target.profiles[0] : null);
  if (!selectedProfile) throw new Error("Connector profile is not loaded.");
  const data = await apiPost(`/api/connector-targets/${target.id}/profiles/${selectedProfile.id}/test`, {});
  return { ok: data.ok, error: data.message || null, data };
}
export function canEdit() { return true; }
export function canDelete() { return true; }
export function credentialHint() { return null; }
export function targetEndpoint({ target }) {
  return targetEndpointValue(target);
}
function targetEndpointValue(target) {
  const brokers = brokerEndpoint(target);
  const mode = target.config?.connection_mode === "over_ssh" ? "over ssh" : "direct";
  return `${brokers} · ${mode}`;
}
export function targetDisplayName({ target }) { return target?.target_name || target?.name || "Kafka target"; }
export function targetSubtitle({ target }) { return targetEndpoint({ target }); }
export function targetProfileLabel({ target }) { return target?.profile_label || "monitor"; }
export function usesLiveConsole() { return false; }
export function recoverableRunningActions() { return []; }
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
export function operationFromError() { return null; }

async function createTarget({ form }) {
  await createTargetWithProfile({
    projectID: form.project_id,
    targetPayload: { connector_kind: "kafka", name: form.name, config: targetConfig(form) },
    profilePayload: credentialPayload(form),
  });
}
async function updateTarget({ form, target }) {
  const profile = target?.profiles?.find((item) => Number(item.id) === Number(form.profile_id)) || (target?.profiles?.length === 1 ? target.profiles[0] : null);
  if (!target || !profile) throw new Error("Kafka connector profile is not loaded.");
  await updateTargetWithProfile({
    projectID: form.project_id,
    targetID: target.id,
    previousTarget: target,
    profileID: profile.id,
    targetPayload: { name: form.name, config: targetConfig(form) },
    profilePayload: credentialPayload(form, profile.kind),
  });
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
