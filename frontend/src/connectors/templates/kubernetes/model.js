import { apiDelete, apiPost, apiPut } from "../../../lib/api";
import { createTargetWithProfile, updateTargetWithProfile } from "../target-profile-save";

const emptyKubernetesCredentialForm = { target_id: "", profile_label: "all-namespaces", scope_mode: "all", namespaces: "", risk_label: "cluster visibility" };

export function emptyForm() {
  return {
    connector_kind: "kubernetes",
    name: "kubernetes",
    connection_mode: "over_ssh",
    transport_target_ref: "",
    kubectl_command: "kubectl",
    context: "",
    default_namespace: "",
    profile_label: "all-namespaces",
    scope_mode: "all",
    namespaces: "",
    risk_label: "cluster visibility",
  };
}

export function formFromTarget({ target, profile }) {
  const selectedProfile = profile || (target?.profiles?.length === 1 ? target.profiles[0] : {});
  return {
    connector_kind: "kubernetes",
    profile_id: selectedProfile.id ? String(selectedProfile.id) : "",
    name: target.name || "",
    connection_mode: target.config?.connection_mode || "over_ssh",
    transport_target_ref: target.config?.transport_target_ref || "",
    kubectl_command: target.config?.kubectl_command || "kubectl",
    context: target.config?.context || "",
    default_namespace: target.config?.default_namespace || "",
    profile_label: selectedProfile.label || "all-namespaces",
    scope_mode: selectedProfile.public?.scope_mode || "all",
    namespaces: selectedProfile.public?.namespaces || "",
    risk_label: selectedProfile.risk_label || "cluster visibility",
  };
}

export function activeCredential() {
  return null;
}

export function syncForm({ form }) {
  if (form.connector_kind !== "kubernetes") return form;
  return { ...form, connection_mode: "over_ssh", kubectl_command: form.kubectl_command || "kubectl" };
}

export function submitDisabled({ state, form }) {
  return state.state === "saving" || !form.transport_target_ref;
}

export function submitLabel({ state, mode }) {
  if (state.state === "saving") return "Saving...";
  return mode === "edit" ? "Save changes" : "Create connector";
}

export async function save({ mode, form, target }) {
  if (mode === "edit") {
    await updateTarget({ form, target });
    return;
  }
  await createTarget({ form });
}

export async function deleteTarget({ target }) {
  await apiDelete(`/api/connector-targets/${target.id}`);
}

export function emptyCredentialState({ targets = [] } = {}) {
  const firstTarget = targets.find((target) => target.connector_kind === "kubernetes");
  return {
    form: {
      ...emptyKubernetesCredentialForm,
      target_id: String(firstTarget?.id || ""),
    },
  };
}

export function credentialStateFromRow({ row }) {
  return {
    form: {
      target_id: String(row.target_id || ""),
      profile_label: row.name,
      scope_mode: row.profile?.public?.scope_mode || "all",
      namespaces: row.profile?.public?.namespaces || "",
      risk_label: row.profile?.risk_label || "",
    },
  };
}

export function credentialFormProps({ targets, formState, setFormState, formMode, state, onSubmit }) {
  return {
    form: formState.form,
    formMode,
    targets,
    state,
    onChange: (form) => setFormState({ form }),
    onSubmit: (event) => onSubmit(event, formMode === "edit" ? "update" : "create"),
  };
}

export async function saveCredential({ operation, row, formState }) {
  const form = formState.form;
  const payload = {
    kind: "namespace_scope",
    label: form.profile_label,
    public: {
      scope_mode: form.scope_mode || "all",
      namespaces: form.namespaces || "",
    },
    secret: {},
    risk_label: form.risk_label,
  };
  if (operation === "create") {
    await apiPost(`/api/connector-targets/${form.target_id}/profiles`, payload);
    return { message: "Kubernetes namespace scope created." };
  }
  if (operation === "update") {
    if (!row) throw new Error("Kubernetes namespace scope is not loaded.");
    await apiPut(`/api/connector-targets/${form.target_id}/profiles/${row.id}`, payload);
    return { message: "Kubernetes namespace scope updated." };
  }
  throw new Error("Unsupported Kubernetes credential operation.");
}

export async function deleteCredential({ row }) {
  await apiDelete(`/api/connector-targets/${row.target_id}/profiles/${row.id}`);
}

export function credentialRows({ targets }) {
  return targets.flatMap((target) =>
    (target.profiles || [])
      .filter((profile) => target.connector_kind === "kubernetes")
      .map((profile) => ({
        row_id: `${target.connector_kind}:${target.id}:${profile.id}`,
        connector_kind: target.connector_kind,
        resource_kind: "credential_profile",
        connector_label: "Kubernetes",
        id: profile.id,
        target_id: target.id,
        name: profile.label,
        kind: profile.kind,
        profile,
        target_label: target.name,
        target_detail: targetEndpoint({ target }),
        metadata: credentialMetadata(profile),
        delete_disabled: "",
      }))
  );
}

export async function test({ target, profile }) {
  const selectedProfile = profile || (target?.profiles?.length === 1 ? target.profiles[0] : null);
  if (!selectedProfile) throw new Error("Kubernetes profile is not loaded.");
  const data = await apiPost(`/api/connector-targets/${target.id}/profiles/${selectedProfile.id}/test`, {});
  return { ok: data.ok, error: data.message || null, data };
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
  const profile = target.config?.transport_target_ref || "no transport";
  const context = target.config?.context ? ` · context ${target.config.context}` : "";
  const namespace = target.config?.default_namespace ? ` · ns ${target.config.default_namespace}` : "";
  return `${target.config?.kubectl_command || "kubectl"} · ${profile}${context}${namespace}`;
}

export function targetDisplayName({ target }) {
  if (!target) return "Kubernetes target";
  return target.target_name || target.name || "Kubernetes target";
}

export function targetSubtitle({ target }) {
  return targetEndpoint({ target });
}

export function targetProfileLabel({ target }) {
  return target?.profile_label || "namespace scope";
}

export function usesLiveConsole() {
  return true;
}

export function liveConsoleRuntimeTarget({ target }) {
  return {
    id: target.runtime_id,
    name: targetDisplayName({ target }),
    host: target.config?.transport_target_ref || "",
    port: 0,
    username: target.profile_label || "",
    description: "Kubernetes pod console",
    connector_ref: target.ref,
    connector_kind: target.connector_kind,
    target_id: target.target_id,
    profile_id: target.profile_id,
    target,
  };
}

export function deleteDialog({ target }) {
  return {
    title: target ? `Delete ${target.name}` : "Delete connector",
    description: "Remove this Kubernetes connector target, namespace scopes, and token action permissions from aipermission.",
    details: [
      { label: "Connector", value: target?.name },
      { label: "Reference", value: target ? `${target.connector_kind}:${target.id}` : "" },
    ],
    notice: "This removes the connector target and its local permission metadata. It does not change the Kubernetes cluster.",
    actions: [
      { label: "Cancel", action: "close", variant: "outline" },
      { label: "Delete connector", pendingLabel: "Deleting...", removeKey: false },
    ],
  };
}

async function createTarget({ form }) {
  await createTargetWithProfile({
    targetPayload: {
      connector_kind: "kubernetes",
      name: form.name,
      config: kubernetesTargetConfigFromForm(form),
    },
    profilePayload: kubernetesProfilePayloadFromForm(form),
  });
}

async function updateTarget({ form, target }) {
  const profile = target?.profiles?.find((item) => Number(item.id) === Number(form.profile_id)) || (target?.profiles?.length === 1 ? target.profiles[0] : null);
  if (!target || !profile) throw new Error("Kubernetes connector profile is not loaded.");
  await updateTargetWithProfile({
    targetID: target.id,
    profileID: profile.id,
    targetPayload: {
      name: form.name,
      config: kubernetesTargetConfigFromForm(form),
    },
    profilePayload: kubernetesProfilePayloadFromForm(form, profile.kind || "namespace_scope"),
  });
}

function kubernetesTargetConfigFromForm(form) {
  return {
    connection_mode: "over_ssh",
    transport_target_ref: form.transport_target_ref || "",
    kubectl_command: form.kubectl_command || "kubectl",
    context: form.context || "",
    default_namespace: form.default_namespace || "",
  };
}

function kubernetesProfilePayloadFromForm(form, kind = "namespace_scope") {
  return {
    kind,
    label: form.profile_label,
    public: {
      scope_mode: form.scope_mode || "all",
      namespaces: form.namespaces || "",
    },
    secret: {},
    risk_label: form.risk_label || "cluster visibility",
  };
}

function credentialMetadata(profile) {
  const scope = profile.public?.scope_mode === "selected" ? "selected namespaces" : "all namespaces";
  const namespaces = splitLines(profile.public?.namespaces || "");
  const items = [`scope: ${scope}`];
  if (namespaces.length > 0) items.push(`namespaces: ${namespaces.slice(0, 3).join(", ")}${namespaces.length > 3 ? ` +${namespaces.length - 3}` : ""}`);
  if (profile.risk_label) items.push(`risk: ${profile.risk_label}`);
  return items;
}

function splitLines(value) {
  return String(value || "")
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean);
}
