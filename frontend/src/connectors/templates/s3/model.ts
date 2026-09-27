import {
  connectorCredentialRows,
  firstTargetCredentialForm,
  standardSubmitLabel,
  createTargetProfileLifecycle,
  defaultTargetProfile,
} from "../_shared/target-profile-lifecycle";
import type { ConnectorDeleteDialog } from "../../editor/connector-editor-dialog-types";

export type S3Profile = {
  id: number;
  label: string;
  kind: string;
  risk_label?: string;
  public?: { access_key_id?: string };
};

export type S3Target = {
  id?: number;
  name: string;
  connector_kind?: string;
  target_name?: string;
  profile_label?: string;
  profiles?: S3Profile[];
  config?: {
    connection_mode?: string;
    scheme?: string;
    host?: string;
    port?: number | string;
    region?: string;
    bucket?: string;
    path_style?: boolean;
    trust_conditional_requests?: boolean;
    transport_target_ref?: string;
  };
};

export type S3PresentationTarget = Pick<S3Target, "config" | "target_name" | "profile_label"> & { name?: string };

export type S3Form = {
  connector_kind: string;
  name: string;
  connection_mode: string;
  scheme: string;
  host: string;
  port: number | string;
  region: string;
  bucket: string;
  path_style: boolean;
  trust_conditional_requests: boolean;
  transport_target_ref: string;
  profile_label: string;
  access_key_id: string;
  secret_access_key: string;
  session_token: string;
  risk_label: string;
  profile_id?: string;
  project_id?: string | number;
};

type S3CredentialForm = Pick<S3Form, "profile_label" | "access_key_id" | "secret_access_key" | "session_token" | "risk_label"> & {
  target_id: string;
};

type S3CredentialRow = {
  target_id: number;
  name: string;
  profile?: S3Profile;
};

const emptyS3CredentialForm = {
  target_id: "",
  profile_label: "default",
  access_key_id: "",
  secret_access_key: "",
  session_token: "",
  risk_label: "object storage",
};
const lifecycle = createTargetProfileLifecycle<S3Form, S3CredentialForm, S3Profile, S3Target>({
  connectorKind: "s3",
  connectorLabel: "S3",
  targetPayload: (form: S3Form) => ({ name: form.name, config: s3TargetConfigFromForm(form) }),
  profilePayload: s3ProfilePayloadFromForm,
});

export const { credentialFormProps, deleteCredential, deleteTarget, save, saveCredential, test } = lifecycle;

export function emptyForm(): S3Form {
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

export function formFromTarget({ target, profile }: { target?: S3Target | null; profile?: S3Profile | null }) {
  const selectedProfile = defaultTargetProfile(target, profile);
  const config = target?.config || {};
  const profilePublic = selectedProfile.public || {};
  return {
    connector_kind: "s3",
    profile_id: selectedProfile.id ? String(selectedProfile.id) : "",
    name: target?.name || "",
    connection_mode: config.connection_mode || "direct",
    scheme: config.scheme || "https",
    host: config.host || "s3.amazonaws.com",
    port: config.port || 443,
    region: config.region || "us-east-1",
    bucket: config.bucket || "",
    path_style: config.path_style !== false,
    trust_conditional_requests: config.trust_conditional_requests === true,
    transport_target_ref: config.transport_target_ref || "",
    profile_label: selectedProfile.label || "default",
    access_key_id: profilePublic.access_key_id || "",
    secret_access_key: "",
    session_token: "",
    risk_label: selectedProfile.risk_label || "object storage",
  };
}

export function activeCredential() {
  return null;
}

export function syncForm({ form }: { form: S3Form }) {
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

export function submitDisabled({ state }: { state: { state: string } }) {
  return state.state === "saving";
}

export function submitLabel({ state, mode }: { state: { state: string }; mode: string }) {
  return standardSubmitLabel({ state, mode });
}

export function emptyCredentialState({ targets = [] }: { targets?: S3Target[] } = {}) {
  return firstTargetCredentialForm(targets, "s3", emptyS3CredentialForm);
}

export function credentialStateFromRow({ row }: { row: S3CredentialRow }): { form: S3CredentialForm } {
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

export function credentialRows<Profile extends S3Profile, Target extends S3Target & { profiles?: Profile[] }>({
  targets,
}: {
  targets: Target[];
}) {
  return connectorCredentialRows<Profile, Target, string>({
    targets,
    connectorKind: "s3",
    connectorLabel: "S3",
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

export function targetEndpoint({ target }: { target: S3PresentationTarget }) {
  const scheme = target.config?.scheme || "https";
  const host = target.config?.host || "s3.amazonaws.com";
  const port = target.config?.port || (scheme === "http" ? 80 : 443);
  const bucket = target.config?.bucket || "bucket";
  const mode = target.config?.connection_mode === "over_ssh" ? "over ssh" : "direct";
  return `${scheme}://${host}:${port}/${bucket} · ${mode}`;
}

export function targetDisplayName({ target }: { target?: S3PresentationTarget | null }) {
  if (!target) return "S3 target";
  return target.target_name || target.name || "S3 target";
}

export function targetSubtitle({ target }: { target: S3PresentationTarget }) {
  return targetEndpoint({ target });
}

export function targetProfileLabel({ target }: { target?: S3PresentationTarget | null }) {
  return target?.profile_label || "default";
}

export function usesLiveConsole() {
  return false;
}

export function recoverableRunningActions() {
  return [];
}

export function deleteDialog({ target }: { target?: S3Target | null }): ConnectorDeleteDialog {
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

export function s3TargetConfigFromForm(form: S3Form) {
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

function s3ProfilePayloadFromForm(
  form: S3Form | S3CredentialForm,
  { profile, operation }: { profile: S3Profile | null; operation: string },
) {
  const secret = s3SecretPayload(form);
  return {
    kind: profile?.kind || "access_key",
    label: form.profile_label,
    public: { access_key_id: form.access_key_id },
    ...(operation.endsWith("create") || Object.keys(secret).length > 0 ? { secret } : {}),
    risk_label: operation.startsWith("target") ? form.risk_label || "object storage" : form.risk_label,
  };
}

function s3SecretPayload(form: S3Form | S3CredentialForm) {
  const secret: { secret_access_key?: string; session_token?: string } = {};
  if (form.secret_access_key) {
    secret.secret_access_key = form.secret_access_key;
  }
  if (form.session_token) {
    secret.session_token = form.session_token;
  }
  return secret;
}

function credentialMetadata(profile: S3Profile) {
  const values = [];
  if (profile.public?.access_key_id) {
    values.push(`access ${maskAccessKey(profile.public.access_key_id)}`);
  }
  if (profile.risk_label) {
    values.push(profile.risk_label);
  }
  return values.join(" · ");
}

function maskAccessKey(value: string) {
  const text = String(value || "");
  if (text.length <= 8) return text;
  return `${text.slice(0, 4)}...${text.slice(-4)}`;
}
