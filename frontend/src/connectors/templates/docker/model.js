import { apiDelete, apiPost, apiPut } from "../../../lib/api";
import { createTargetWithProfile, updateTargetWithProfile } from "../target-profile-save";

const emptyDockerCredentialForm = { target_id: "", profile_label: "all-containers", scope_mode: "all", allowed_containers: "", allowed_patterns: "", risk_label: "container access" };

export function emptyForm() {
  return {
    connector_kind: "docker",
    name: "docker-host",
    connection_mode: "over_ssh",
    transport_target_ref: "",
    docker_command: "docker",
    profile_label: "all-containers",
    scope_mode: "all",
    allowed_containers: "",
    allowed_patterns: "",
    risk_label: "container access",
  };
}

export function formFromTarget({ target, profile }) {
  const selectedProfile = profile || (target?.profiles?.length === 1 ? target.profiles[0] : {});
  return {
    connector_kind: "docker",
    profile_id: selectedProfile.id ? String(selectedProfile.id) : "",
    name: target.name || "",
    connection_mode: target.config?.connection_mode || "over_ssh",
    transport_target_ref: target.config?.transport_target_ref || "",
    docker_command: target.config?.docker_command || "docker",
    profile_label: selectedProfile.label || "all-containers",
    scope_mode: selectedProfile.public?.scope_mode || "all",
    allowed_containers: selectedProfile.public?.allowed_containers || "",
    allowed_patterns: selectedProfile.public?.allowed_patterns || "",
    risk_label: selectedProfile.risk_label || "container access",
  };
}

export function activeCredential() {
  return null;
}

export function syncForm({ form }) {
  if (form.connector_kind !== "docker") return form;
  return { ...form, connection_mode: "over_ssh", docker_command: form.docker_command || "docker" };
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
  const firstTarget = targets.find((target) => target.connector_kind === "docker");
  return {
    form: {
      ...emptyDockerCredentialForm,
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
      allowed_containers: row.profile?.public?.allowed_containers || "",
      allowed_patterns: row.profile?.public?.allowed_patterns || "",
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
    kind: "container_scope",
    label: form.profile_label,
    public: {
      scope_mode: form.scope_mode || "all",
      allowed_containers: form.allowed_containers || "",
      allowed_patterns: form.allowed_patterns || "",
    },
    secret: {},
    risk_label: form.risk_label,
  };
  if (operation === "create") {
    await apiPost(`/api/connector-targets/${form.target_id}/profiles`, payload);
    return { message: "Docker credential scope created." };
  }
  if (operation === "update") {
    if (!row) throw new Error("Docker credential scope is not loaded.");
    await apiPut(`/api/connector-targets/${form.target_id}/profiles/${row.id}`, payload);
    return { message: "Docker credential scope updated." };
  }
  throw new Error("Unsupported Docker credential operation.");
}

export async function deleteCredential({ row }) {
  await apiDelete(`/api/connector-targets/${row.target_id}/profiles/${row.id}`);
}

export function credentialRows({ targets }) {
  return targets.flatMap((target) =>
    (target.profiles || [])
      .filter((profile) => target.connector_kind === "docker")
      .map((profile) => ({
        row_id: `${target.connector_kind}:${target.id}:${profile.id}`,
        connector_kind: target.connector_kind,
        resource_kind: "credential_profile",
        connector_label: "Docker",
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
  if (!selectedProfile) throw new Error("Docker profile is not loaded.");
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
  return `${target.config?.docker_command || "docker"} · ${profile}`;
}

export function targetDisplayName({ target }) {
  if (!target) return "Docker target";
  return target.target_name || target.name || "Docker target";
}

export function targetSubtitle({ target }) {
  return targetEndpoint({ target });
}

export function targetProfileLabel({ target }) {
  return target?.profile_label || "container scope";
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
    description: "Docker container console",
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
    description: "Remove this Docker connector target, credential scopes, and token action permissions from aipermission.",
    details: [
      { label: "Connector", value: target?.name },
      { label: "Reference", value: target ? `${target.connector_kind}:${target.id}` : "" },
    ],
    notice: "This removes the connector target and its local permission metadata. It does not change Docker containers.",
    actions: [
      { label: "Cancel", action: "close", variant: "outline" },
      { label: "Delete connector", pendingLabel: "Deleting...", removeKey: false },
    ],
  };
}

async function createTarget({ form }) {
  await createTargetWithProfile({
    targetPayload: {
      connector_kind: "docker",
      name: form.name,
      config: dockerTargetConfigFromForm(form),
    },
    profilePayload: dockerProfilePayloadFromForm(form),
  });
}

async function updateTarget({ form, target }) {
  const profile = target?.profiles?.find((item) => Number(item.id) === Number(form.profile_id)) || (target?.profiles?.length === 1 ? target.profiles[0] : null);
  if (!target || !profile) throw new Error("Docker connector profile is not loaded.");
  await updateTargetWithProfile({
    targetID: target.id,
    profileID: profile.id,
    targetPayload: {
      name: form.name,
      config: dockerTargetConfigFromForm(form),
    },
    profilePayload: dockerProfilePayloadFromForm(form, profile.kind || "container_scope"),
  });
}

function dockerTargetConfigFromForm(form) {
  return {
    connection_mode: "over_ssh",
    transport_target_ref: form.transport_target_ref || "",
    docker_command: form.docker_command || "docker",
  };
}

function dockerProfilePayloadFromForm(form, kind = "container_scope") {
  return {
    kind,
    label: form.profile_label,
    public: {
      scope_mode: form.scope_mode || "all",
      allowed_containers: form.allowed_containers || "",
      allowed_patterns: form.allowed_patterns || "",
    },
    secret: {},
    risk_label: form.risk_label || "container access",
  };
}

function credentialMetadata(profile) {
  const scope = profile.public?.scope_mode === "selected" ? "selected containers" : "all containers";
  const names = splitLines(profile.public?.allowed_containers || "");
  const patterns = splitLines(profile.public?.allowed_patterns || "");
  const items = [`scope: ${scope}`];
  if (names.length > 0) items.push(`names: ${names.slice(0, 3).join(", ")}${names.length > 3 ? ` +${names.length - 3}` : ""}`);
  if (patterns.length > 0) items.push(`patterns: ${patterns.slice(0, 3).join(", ")}${patterns.length > 3 ? ` +${patterns.length - 3}` : ""}`);
  if (profile.risk_label) items.push(`risk: ${profile.risk_label}`);
  return items;
}

function splitLines(value) {
  return String(value || "")
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean);
}
