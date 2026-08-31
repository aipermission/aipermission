import { connectorCredentialRows, createTargetProfileLifecycle } from "../_shared/target-profile-lifecycle";

const emptyDockerCredentialForm = {
  target_id: "",
  profile_label: "all-containers",
  scope_mode: "all",
  allowed_containers: "",
  allowed_patterns: "",
  risk_label: "container access",
};
const lifecycle = createTargetProfileLifecycle({
  connectorKind: "docker",
  connectorLabel: "Docker",
  targetPayload: (form) => ({ name: form.name, config: dockerTargetConfigFromForm(form) }),
  profilePayload: (form, { profile, operation }) =>
    dockerProfilePayloadFromForm(
      form,
      operation === "target-update" ? profile?.kind || "container_scope" : "container_scope",
      operation.startsWith("target"),
    ),
  credentialCreatedMessage: "Docker credential scope created.",
  credentialUpdatedMessage: "Docker credential scope updated.",
  credentialMissingMessage: "Docker credential scope is not loaded.",
});

export const { credentialFormProps, deleteCredential, deleteTarget, save, saveCredential, test } = lifecycle;

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

export function credentialRows({ targets }) {
  return connectorCredentialRows({ targets, connectorKind: "docker", connectorLabel: "Docker", targetEndpoint, credentialMetadata });
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

export function recoverableRunningActions() {
  return [];
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

function dockerTargetConfigFromForm(form) {
  return {
    connection_mode: "over_ssh",
    transport_target_ref: form.transport_target_ref || "",
    docker_command: form.docker_command || "docker",
  };
}

function dockerProfilePayloadFromForm(form, kind = "container_scope", useDefaultRisk = true) {
  return {
    kind,
    label: form.profile_label,
    public: {
      scope_mode: form.scope_mode || "all",
      allowed_containers: form.allowed_containers || "",
      allowed_patterns: form.allowed_patterns || "",
    },
    secret: {},
    risk_label: useDefaultRisk ? form.risk_label || "container access" : form.risk_label,
  };
}

function credentialMetadata(profile) {
  const scope = profile.public?.scope_mode === "selected" ? "selected containers" : "all containers";
  const names = splitLines(profile.public?.allowed_containers || "");
  const patterns = splitLines(profile.public?.allowed_patterns || "");
  const items = [`scope: ${scope}`];
  if (names.length > 0) items.push(`names: ${names.slice(0, 3).join(", ")}${names.length > 3 ? ` +${names.length - 3}` : ""}`);
  if (patterns.length > 0)
    items.push(`patterns: ${patterns.slice(0, 3).join(", ")}${patterns.length > 3 ? ` +${patterns.length - 3}` : ""}`);
  if (profile.risk_label) items.push(`risk: ${profile.risk_label}`);
  return items;
}

function splitLines(value) {
  return String(value || "")
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean);
}
