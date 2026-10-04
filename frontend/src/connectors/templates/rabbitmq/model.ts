import {
  connectorCredentialRows,
  firstTargetCredentialForm,
  usernameCredentialStateFromRow,
  standardSubmitLabel,
  createTargetProfileLifecycle,
  defaultTargetProfile,
} from "../_shared/target-profile-lifecycle";
import type { RabbitMQModelForm, RabbitMQPresentationTarget, RabbitMQProfile, RabbitMQTarget } from "./form-types";
import type { UsernameCredentialForm } from "../_shared/connector-form-types";
import type { LifecycleProfileOperation } from "../../profile-lifecycle/types";
import { createStructuredConsoleModel } from "../_shared/structured-console-model";
import type { ConnectorDeleteDialog } from "../../editor/connector-editor-dialog-types";

const emptyRabbitCredentialForm = { target_id: "", profile_label: "monitor", username: "", password: "", risk_label: "queue access" };
const lifecycle = createTargetProfileLifecycle<RabbitMQModelForm, UsernameCredentialForm, RabbitMQProfile, RabbitMQTarget>({
  connectorKind: "rabbitmq",
  connectorLabel: "RabbitMQ",
  targetPayload: (form) => ({ name: form.name, config: rabbitTargetConfigFromForm(form) }),
  profilePayload: rabbitProfilePayloadFromForm,
});

export const { credentialFormProps, deleteCredential, deleteTarget, save, saveCredential, test } = lifecycle;

export function emptyForm(): RabbitMQModelForm {
  return {
    connector_kind: "rabbitmq",
    name: "rabbitmq",
    connection_mode: "direct",
    scheme: "auto",
    host: "127.0.0.1",
    port: 15672,
    vhost: "/",
    transport_target_ref: "",
    profile_label: "monitor",
    username: "",
    password: "",
    risk_label: "queue access",
  };
}

export function formFromTarget({ target, profile }: { target: RabbitMQTarget; profile?: RabbitMQProfile | null }): RabbitMQModelForm {
  const selectedProfile = defaultTargetProfile(target, profile);
  return {
    connector_kind: "rabbitmq",
    profile_id: selectedProfile.id ? String(selectedProfile.id) : "",
    name: target.name || "",
    connection_mode: target.config?.connection_mode || "direct",
    scheme: target.config?.scheme || "http",
    host: target.config?.host || "127.0.0.1",
    port: target.config?.port || 15672,
    vhost: target.config?.vhost || "/",
    transport_target_ref: target.config?.transport_target_ref || "",
    profile_label: selectedProfile.label || "monitor",
    username: selectedProfile.public?.username || "",
    password: "",
    risk_label: selectedProfile.risk_label || "queue access",
  };
}

export function activeCredential() {
  return null;
}

export function syncForm({ form }: { form: RabbitMQModelForm }) {
  if (form.connector_kind !== "rabbitmq") return form;
  const next = { ...form };
  if (next.connection_mode === "direct") {
    next.transport_target_ref = "";
  }
  if (!next.scheme) {
    next.scheme = "http";
  }
  if (!next.vhost) {
    next.vhost = "/";
  }
  return next;
}

export function submitDisabled({ state }: { state: { state: string } }) {
  return state.state === "saving";
}

export function submitLabel({ state, mode }: { state: { state: string }; mode: string }) {
  return standardSubmitLabel({ state, mode });
}

export function emptyCredentialState({ targets = [] }: { targets?: RabbitMQTarget[] } = {}) {
  return firstTargetCredentialForm(targets, "rabbitmq", emptyRabbitCredentialForm);
}

export function credentialStateFromRow({ row }: { row: { target_id: number; name: string; profile?: RabbitMQProfile } }) {
  return usernameCredentialStateFromRow(row);
}

export function credentialRows<Profile extends RabbitMQProfile, Target extends RabbitMQTarget & { profiles?: Profile[] }>({
  targets,
}: {
  targets: Target[];
}) {
  return connectorCredentialRows<Profile, Target, string[]>({
    targets,
    connectorKind: "rabbitmq",
    connectorLabel: "RabbitMQ",
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

export function targetEndpoint({ target }: { target: RabbitMQPresentationTarget }) {
  const scheme = target.config?.scheme || "http";
  const host = target.config?.host || "127.0.0.1";
  const port = target.config?.port || 15672;
  const vhost = target.config?.vhost || "/";
  const mode = target.config?.connection_mode === "over_ssh" ? "over ssh" : "direct";
  return `${scheme}://${host}:${port} · vhost ${vhost} · ${mode}`;
}

export function targetDisplayName({ target }: { target?: RabbitMQPresentationTarget | null }) {
  if (!target) return "RabbitMQ target";
  return target.target_name || target.name || "RabbitMQ target";
}

export function targetSubtitle({ target }: { target: RabbitMQPresentationTarget }) {
  return targetEndpoint({ target });
}

export const { targetProfileLabel, usesLiveConsole, recoverableRunningActions } = createStructuredConsoleModel("monitor");

export function deleteDialog({ target }: { target?: RabbitMQTarget | null }): ConnectorDeleteDialog {
  return {
    title: target ? `Delete ${target.name}` : "Delete connector",
    description: "Remove this RabbitMQ connector target, credential profiles, and token action permissions from aipermission.",
    details: [
      { label: "Connector", value: target?.name },
      { label: "Reference", value: target ? `${target.connector_kind}:${target.id}` : "" },
    ],
    notice: "This removes the connector target and its credential profiles. It does not change the RabbitMQ server.",
    actions: [
      { label: "Cancel", action: "close", variant: "outline" },
      { label: "Delete connector", pendingLabel: "Deleting...", removeKey: false },
    ],
  };
}

export function operationFromError() {
  return null;
}

function rabbitTargetConfigFromForm(form: RabbitMQModelForm) {
  const scheme = ["auto", "https"].includes(form.scheme || "") ? form.scheme : "http";
  return {
    connection_mode: form.connection_mode || "direct",
    scheme,
    host: form.host || "127.0.0.1",
    port: Number(form.port) || 15672,
    vhost: form.vhost || "/",
    transport_target_ref: form.connection_mode === "over_ssh" ? form.transport_target_ref || "" : "",
  };
}

function rabbitProfilePayloadFromForm(
  form: RabbitMQModelForm | UsernameCredentialForm,
  { profile, operation }: { profile: RabbitMQProfile | null; operation: LifecycleProfileOperation },
) {
  return {
    kind: profile?.kind || "username_password",
    label: form.profile_label,
    public: { username: form.username },
    ...(form.password ? { secret: { password: form.password } } : {}),
    risk_label: operation.startsWith("target") ? form.risk_label || "queue access" : form.risk_label,
  };
}

function credentialMetadata(profile: RabbitMQProfile) {
  const items = [];
  if (profile.public?.username) items.push(`username: ${profile.public.username}`);
  if (profile.risk_label) items.push(`risk: ${profile.risk_label}`);
  if (items.length === 0) items.push("No public metadata");
  return items;
}
