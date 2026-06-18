import { apiDelete, apiPost, apiPut } from "../../../lib/api";
import { createTargetWithProfile, updateTargetWithProfile } from "../target-profile-save";

const emptyRedisCredentialForm = { target_id: "", profile_label: "default", username: "", password: "", risk_label: "cache access" };

export function emptyForm() {
  return {
    connector_kind: "redis",
    name: "redis-cache",
    connection_mode: "direct",
    host: "127.0.0.1",
    port: 6379,
    database: 0,
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
    connection_mode: target.config?.connection_mode || "direct",
    host: target.config?.host || "127.0.0.1",
    port: target.config?.port || 6379,
    database: target.config?.database || 0,
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
  if (operation === "create") {
    await apiPost(`/api/connector-targets/${form.target_id}/profiles`, {
      kind: "username_password",
      label: form.profile_label,
      public: { username: form.username },
      secret: form.password ? { password: form.password } : {},
      risk_label: form.risk_label,
    });
    return { message: "Redis credential created." };
  }
  if (operation === "update") {
    if (!row) throw new Error("Redis credential is not loaded.");
    const payload = {
      kind: row.profile?.kind || "username_password",
      label: form.profile_label,
      public: { username: form.username },
      risk_label: form.risk_label,
    };
    if (form.password) {
      payload.secret = { password: form.password };
    }
    await apiPut(`/api/connector-targets/${form.target_id}/profiles/${row.id}`, payload);
    return { message: "Redis credential updated." };
  }
  throw new Error("Unsupported Redis credential operation.");
}

export async function deleteCredential({ row }) {
  await apiDelete(`/api/connector-targets/${row.target_id}/profiles/${row.id}`);
}

export function credentialRows({ targets }) {
  return targets.flatMap((target) =>
    (target.profiles || [])
      .filter((profile) => target.connector_kind === "redis")
      .map((profile) => ({
        row_id: `${target.connector_kind}:${target.id}:${profile.id}`,
        connector_kind: target.connector_kind,
        resource_kind: "credential_profile",
        connector_label: "Redis",
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
  if (!selectedProfile) throw new Error("Connector profile is not loaded.");
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
  const host = target.config?.host || "127.0.0.1";
  const port = target.config?.port || 6379;
  const database = target.config?.database || 0;
  const mode = target.config?.connection_mode === "over_ssh" ? "over ssh" : "direct";
  return `${host}:${port}/${database} · ${mode}`;
}

export function targetDisplayName({ target }) {
  if (!target) return "Redis target";
  return target.target_name || target.name || "Redis target";
}

export function targetSubtitle({ target }) {
  return targetEndpoint({ target });
}

export function targetProfileLabel({ target }) {
  return target?.profile_label || "default";
}

export function usesLiveConsole() {
  return false;
}

export function deleteDialog({ target }) {
  return {
    title: target ? `Delete ${target.name}` : "Delete connector",
    description: "Remove this Redis connector target, credential profiles, and token action permissions from aipermission.",
    details: [
      { label: "Connector", value: target?.name },
      { label: "Reference", value: target ? `${target.connector_kind}:${target.id}` : "" },
    ],
    notice: "This removes the connector target and its credential profiles. It does not change the Redis server.",
    actions: [
      { label: "Cancel", action: "close", variant: "outline" },
      { label: "Delete connector", pendingLabel: "Deleting...", removeKey: false },
    ],
  };
}

export function operationFromError() {
  return null;
}

async function createTarget({ form }) {
  await createTargetWithProfile({
    targetPayload: {
      connector_kind: "redis",
      name: form.name,
      config: redisTargetConfigFromForm(form),
    },
    profilePayload: {
      kind: "username_password",
      label: form.profile_label,
      public: { username: form.username },
      secret: form.password ? { password: form.password } : {},
      risk_label: form.risk_label || "cache access",
    },
  });
}

async function updateTarget({ form, target }) {
  const profile = target?.profiles?.find((item) => Number(item.id) === Number(form.profile_id)) || (target?.profiles?.length === 1 ? target.profiles[0] : null);
  if (!target || !profile) throw new Error("Redis connector profile is not loaded.");
  const profilePayload = {
    kind: profile.kind || "username_password",
    label: form.profile_label,
    public: { username: form.username },
    risk_label: form.risk_label || "cache access",
  };
  if (form.password) {
    profilePayload.secret = { password: form.password };
  }
  await updateTargetWithProfile({
    targetID: target.id,
    previousTarget: target,
    profileID: profile.id,
    targetPayload: {
      name: form.name,
      config: redisTargetConfigFromForm(form),
    },
    profilePayload,
  });
}

function redisTargetConfigFromForm(form) {
  return {
    connection_mode: form.connection_mode || "direct",
    host: form.host || "127.0.0.1",
    port: Number(form.port) || 6379,
    database: Number(form.database) || 0,
    transport_target_ref: form.connection_mode === "over_ssh" ? form.transport_target_ref || "" : "",
  };
}

function credentialMetadata(profile) {
  const items = [];
  if (profile.public?.username) items.push(`username: ${profile.public.username}`);
  if (profile.risk_label) items.push(`risk: ${profile.risk_label}`);
  if (items.length === 0) items.push("No public metadata");
  return items;
}
