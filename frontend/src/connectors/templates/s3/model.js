import { apiDelete, apiPost, apiPut } from "../../../lib/api";
import { createTargetWithProfile, updateTargetWithProfile } from "../target-profile-save";

const emptyS3CredentialForm = { target_id: "", profile_label: "default", access_key_id: "", secret_access_key: "", session_token: "", risk_label: "object storage" };

export function emptyForm() {
  return {
    connector_kind: "s3",
    name: "object-store",
    connection_mode: "direct",
    scheme: "https",
    host: "s3.amazonaws.com",
    port: 443,
    region: "us-east-1",
    bucket: "",
    path_style: true,
    transport_target_ref: "",
    profile_label: "default",
    access_key_id: "",
    secret_access_key: "",
    session_token: "",
    risk_label: "object storage",
  };
}

export function formFromTarget({ target, profile }) {
  const selectedProfile = profile || (target?.profiles?.length === 1 ? target.profiles[0] : {});
  return {
    connector_kind: "s3",
    profile_id: selectedProfile.id ? String(selectedProfile.id) : "",
    name: target.name || "",
    connection_mode: target.config?.connection_mode || "direct",
    scheme: target.config?.scheme || "https",
    host: target.config?.host || "s3.amazonaws.com",
    port: target.config?.port || 443,
    region: target.config?.region || "us-east-1",
    bucket: target.config?.bucket || "",
    path_style: target.config?.path_style !== false,
    transport_target_ref: target.config?.transport_target_ref || "",
    profile_label: selectedProfile.label || "default",
    access_key_id: selectedProfile.public?.access_key_id || "",
    secret_access_key: "",
    session_token: "",
    risk_label: selectedProfile.risk_label || "object storage",
  };
}

export function activeCredential() {
  return null;
}

export function syncForm({ form }) {
  if (form.connector_kind !== "s3") return form;
  const next = { ...form };
  if (next.connection_mode === "direct") {
    next.transport_target_ref = "";
  }
  if (!next.scheme) {
    next.scheme = "https";
  }
  if (!next.port) {
    next.port = next.scheme === "http" ? 80 : 443;
  }
  if (!next.region) {
    next.region = "us-east-1";
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
  const firstTarget = targets.find((target) => target.connector_kind === "s3");
  return {
    form: {
      ...emptyS3CredentialForm,
      target_id: String(firstTarget?.id || ""),
    },
  };
}

export function credentialStateFromRow({ row }) {
  return {
    form: {
      target_id: String(row.target_id || ""),
      profile_label: row.name,
      access_key_id: row.profile?.public?.access_key_id || "",
      secret_access_key: "",
      session_token: "",
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
      kind: "access_key",
      label: form.profile_label,
      public: { access_key_id: form.access_key_id },
      secret: s3SecretPayload(form),
      risk_label: form.risk_label,
    });
    return { message: "S3 credential created." };
  }
  if (operation === "update") {
    if (!row) throw new Error("S3 credential is not loaded.");
    const payload = {
      kind: row.profile?.kind || "access_key",
      label: form.profile_label,
      public: { access_key_id: form.access_key_id },
      risk_label: form.risk_label,
    };
    const secret = s3SecretPayload(form);
    if (Object.keys(secret).length > 0) {
      payload.secret = secret;
    }
    await apiPut(`/api/connector-targets/${form.target_id}/profiles/${row.id}`, payload);
    return { message: "S3 credential updated." };
  }
  throw new Error("Unsupported S3 credential operation.");
}

export async function deleteCredential({ row }) {
  await apiDelete(`/api/connector-targets/${row.target_id}/profiles/${row.id}`);
}

export function credentialRows({ targets }) {
  return targets.flatMap((target) =>
    (target.profiles || [])
      .filter((profile) => target.connector_kind === "s3")
      .map((profile) => ({
        row_id: `${target.connector_kind}:${target.id}:${profile.id}`,
        connector_kind: target.connector_kind,
        resource_kind: "credential_profile",
        connector_label: "S3",
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
  const scheme = target.config?.scheme || "https";
  const host = target.config?.host || "s3.amazonaws.com";
  const port = target.config?.port || (scheme === "http" ? 80 : 443);
  const bucket = target.config?.bucket || "bucket";
  const mode = target.config?.connection_mode === "over_ssh" ? "over ssh" : "direct";
  return `${scheme}://${host}:${port}/${bucket} · ${mode}`;
}

export function targetDisplayName({ target }) {
  if (!target) return "S3 target";
  return target.target_name || target.name || "S3 target";
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

export function recoverableRunningActions() {
  return [];
}

export function deleteDialog({ target }) {
  return {
    title: target ? `Delete ${target.name}` : "Delete connector",
    description: "Remove this S3 connector target, credential profiles, and token action permissions from AIPermission.",
    details: [
      { label: "Connector", value: target?.name },
      { label: "Reference", value: target ? `${target.connector_kind}:${target.id}` : "" },
    ],
    notice: "This removes the connector target and its credential profiles. It does not delete buckets or objects.",
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
	projectID: form.project_id,
    targetPayload: {
      connector_kind: "s3",
      name: form.name,
      config: s3TargetConfigFromForm(form),
    },
    profilePayload: {
      kind: "access_key",
      label: form.profile_label,
      public: { access_key_id: form.access_key_id },
      secret: s3SecretPayload(form),
      risk_label: form.risk_label || "object storage",
    },
  });
}

async function updateTarget({ form, target }) {
  const profile = target?.profiles?.find((item) => Number(item.id) === Number(form.profile_id)) || (target?.profiles?.length === 1 ? target.profiles[0] : null);
  if (!target || !profile) throw new Error("S3 connector profile is not loaded.");
  const profilePayload = {
    kind: profile.kind || "access_key",
    label: form.profile_label,
    public: { access_key_id: form.access_key_id },
    risk_label: form.risk_label || "object storage",
  };
  const secret = s3SecretPayload(form);
  if (Object.keys(secret).length > 0) {
    profilePayload.secret = secret;
  }
  await updateTargetWithProfile({
	projectID: form.project_id,
    targetID: target.id,
    profileID: profile.id,
    targetPayload: {
      connector_kind: "s3",
      name: form.name,
      config: s3TargetConfigFromForm(form),
    },
    profilePayload,
  });
}

function s3TargetConfigFromForm(form) {
  return {
    connection_mode: form.connection_mode || "direct",
    scheme: form.scheme || "https",
    host: form.host,
    port: Number(form.port || (form.scheme === "http" ? 80 : 443)),
    region: form.region || "us-east-1",
    bucket: form.bucket,
    path_style: form.path_style !== false,
    transport_target_ref: form.connection_mode === "over_ssh" ? form.transport_target_ref : "",
  };
}

function s3SecretPayload(form) {
  const secret = {};
  if (form.secret_access_key) {
    secret.secret_access_key = form.secret_access_key;
  }
  if (form.session_token) {
    secret.session_token = form.session_token;
  }
  return secret;
}

function credentialMetadata(profile) {
  const values = [];
  if (profile.public?.access_key_id) {
    values.push(`access ${maskAccessKey(profile.public.access_key_id)}`);
  }
  if (profile.risk_label) {
    values.push(profile.risk_label);
  }
  return values.join(" · ");
}

function maskAccessKey(value) {
  const text = String(value || "");
  if (text.length <= 8) return text;
  return `${text.slice(0, 4)}...${text.slice(-4)}`;
}
