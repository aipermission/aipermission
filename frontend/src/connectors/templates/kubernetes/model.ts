import {
  connectorCredentialRows,
  firstTargetCredentialForm,
  standardSubmitLabel,
  createTargetProfileLifecycle,
  defaultTargetProfile,
} from "../_shared/target-profile-lifecycle";
import type {
  KubernetesCredentialForm,
  KubernetesModelForm,
  KubernetesProfile,
  KubernetesRuntimeTarget,
  KubernetesTarget,
} from "./form-types";
import type { ConnectorDeleteDialog } from "../../editor/connector-editor-dialog-types";

const emptyKubernetesCredentialForm = {
  target_id: "",
  profile_label: "all-namespaces",
  scope_mode: "all",
  namespaces: "",
  risk_label: "cluster visibility",
};
const lifecycle = createTargetProfileLifecycle<KubernetesModelForm, KubernetesCredentialForm, KubernetesProfile, KubernetesTarget>({
  connectorKind: "kubernetes",
  connectorLabel: "Kubernetes",
  targetPayload: (form) => ({ name: form.name, config: kubernetesTargetConfigFromForm(form) }),
  profilePayload: (form, { profile, operation }) =>
    kubernetesProfilePayloadFromForm(
      form,
      operation === "target-update" ? profile?.kind || "namespace_scope" : "namespace_scope",
      operation.startsWith("target"),
    ),
  credentialCreatedMessage: "Kubernetes namespace scope created.",
  credentialUpdatedMessage: "Kubernetes namespace scope updated.",
  credentialMissingMessage: "Kubernetes namespace scope is not loaded.",
});

export const { credentialFormProps, deleteCredential, deleteTarget, save, saveCredential, test } = lifecycle;

export function emptyForm(): KubernetesModelForm {
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

export function formFromTarget({ target, profile }: { target: KubernetesTarget; profile?: KubernetesProfile | null }): KubernetesModelForm {
  const selectedProfile = defaultTargetProfile(target, profile);
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

export function syncForm({ form }: { form: KubernetesModelForm }) {
  if (form.connector_kind !== "kubernetes") return form;
  return { ...form, connection_mode: "over_ssh", kubectl_command: form.kubectl_command || "kubectl" };
}

export function submitDisabled({ state, form }: { state: { state: string }; form: KubernetesModelForm }) {
  return state.state === "saving" || !form.transport_target_ref;
}

export function submitLabel({ state, mode }: { state: { state: string }; mode: string }) {
  return standardSubmitLabel({ state, mode });
}

export function emptyCredentialState({ targets = [] }: { targets?: KubernetesTarget[] } = {}) {
  return firstTargetCredentialForm(targets, "kubernetes", emptyKubernetesCredentialForm);
}

export function credentialStateFromRow({ row }: { row: { target_id: number; name: string; profile?: KubernetesProfile } }) {
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

export function credentialRows<Profile extends KubernetesProfile, Target extends KubernetesTarget & { profiles?: Profile[] }>({
  targets,
}: {
  targets: Target[];
}) {
  return connectorCredentialRows<Profile, Target, string[]>({
    targets,
    connectorKind: "kubernetes",
    connectorLabel: "Kubernetes",
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

export function targetEndpoint({ target }: { target: KubernetesTarget }) {
  const profile = target.config?.transport_target_ref || "no transport";
  const context = target.config?.context ? ` · context ${target.config.context}` : "";
  const namespace = target.config?.default_namespace ? ` · ns ${target.config.default_namespace}` : "";
  return `${target.config?.kubectl_command || "kubectl"} · ${profile}${context}${namespace}`;
}

export function targetDisplayName({ target }: { target?: KubernetesTarget | null }) {
  if (!target) return "Kubernetes target";
  return target.target_name || target.name || "Kubernetes target";
}

export function targetSubtitle({ target }: { target: KubernetesTarget }) {
  return targetEndpoint({ target });
}

export function targetProfileLabel({ target }: { target?: KubernetesTarget | null }) {
  return target?.profile_label || "namespace scope";
}

export function usesLiveConsole() {
  return true;
}

export function recoverableRunningActions() {
  return [];
}

export function liveConsoleRuntimeTarget({ target }: { target: KubernetesRuntimeTarget }) {
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

export function deleteDialog({ target }: { target?: KubernetesTarget | null }): ConnectorDeleteDialog {
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

function kubernetesTargetConfigFromForm(form: KubernetesModelForm) {
  return {
    connection_mode: "over_ssh",
    transport_target_ref: form.transport_target_ref || "",
    kubectl_command: form.kubectl_command || "kubectl",
    context: form.context || "",
    default_namespace: form.default_namespace || "",
  };
}

function kubernetesProfilePayloadFromForm(
  form: KubernetesModelForm | KubernetesCredentialForm,
  kind = "namespace_scope",
  useDefaultRisk = true,
) {
  return {
    kind,
    label: form.profile_label,
    public: {
      scope_mode: form.scope_mode || "all",
      namespaces: form.namespaces || "",
    },
    secret: {},
    risk_label: useDefaultRisk ? form.risk_label || "cluster visibility" : form.risk_label,
  };
}

function credentialMetadata(profile: KubernetesProfile) {
  const scope = profile.public?.scope_mode === "selected" ? "selected namespaces" : "all namespaces";
  const namespaces = splitLines(profile.public?.namespaces || "");
  const items = [`scope: ${scope}`];
  if (namespaces.length > 0)
    items.push(`namespaces: ${namespaces.slice(0, 3).join(", ")}${namespaces.length > 3 ? ` +${namespaces.length - 3}` : ""}`);
  if (profile.risk_label) items.push(`risk: ${profile.risk_label}`);
  return items;
}

function splitLines(value: string) {
  return String(value || "")
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean);
}
