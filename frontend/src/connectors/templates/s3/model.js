import { connectorCredentialRows, createTargetProfileLifecycle } from "../_shared/target-profile-lifecycle";

const emptyS3CredentialForm = {
  target_id: "",
  profile_label: "default",
  access_key_id: "",
  secret_access_key: "",
  session_token: "",
  risk_label: "object storage",
};
const lifecycle = createTargetProfileLifecycle({
  connectorKind: "s3",
  connectorLabel: "S3",
  targetPayload: (form) => ({ name: form.name, config: s3TargetConfigFromForm(form) }),
  profilePayload: s3ProfilePayloadFromForm,
});

export const { credentialFormProps, deleteCredential, deleteTarget, save, saveCredential, test } = lifecycle;

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
    trust_conditional_requests: false,
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
    trust_conditional_requests: target.config?.trust_conditional_requests === true,
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

export function credentialRows({ targets }) {
  return connectorCredentialRows({ targets, connectorKind: "s3", connectorLabel: "S3", targetEndpoint, credentialMetadata });
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

export function s3TargetConfigFromForm(form) {
  return {
    connection_mode: form.connection_mode || "direct",
    scheme: form.scheme || "https",
    host: form.host,
    port: Number(form.port || (form.scheme === "http" ? 80 : 443)),
    region: form.region || "us-east-1",
    bucket: form.bucket,
    path_style: form.path_style !== false,
    trust_conditional_requests: form.trust_conditional_requests === true,
    transport_target_ref: form.connection_mode === "over_ssh" ? form.transport_target_ref : "",
  };
}

function s3ProfilePayloadFromForm(form, { profile, operation }) {
  const secret = s3SecretPayload(form);
  return {
    kind: profile?.kind || "access_key",
    label: form.profile_label,
    public: { access_key_id: form.access_key_id },
    ...(operation.endsWith("create") || Object.keys(secret).length > 0 ? { secret } : {}),
    risk_label: operation.startsWith("target") ? form.risk_label || "object storage" : form.risk_label,
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
