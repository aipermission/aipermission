import { apiDelete, apiGet, apiPost, apiPut } from "../../../lib/api";
import { createTargetWithProfile, updateTargetWithProfile } from "../target-profile-save";

const emptySSHCredentialForm = { name: "main", key_type: "ed25519" };
const emptySSHCredentialImportForm = { name: "imported-key", private_key: "", passphrase: "" };

export function emptyForm({ firstCredentialID = "" } = {}) {
  return {
    connector_kind: "ssh",
    name: "",
    host: "",
    port: 22,
    username: "root",
    ssh_key_id: firstCredentialID,
    description: "",
    startup_input_after_connect: "",
    force_shell_command: "",
    setup_later: false,
  };
}

export function formFromTarget({ target, profile, server }) {
  const selectedProfile = profile || (target?.profiles?.length === 1 ? target.profiles[0] : {});
  const profilePublic = selectedProfile.public || {};
  return {
    connector_kind: "ssh",
    profile_id: selectedProfile.id ? String(selectedProfile.id) : "",
    name: target?.name || server?.name || "",
    host: target?.config?.host || server?.host || "",
    port: target?.config?.port || server?.port || 22,
    username: profilePublic.username || server?.username || "root",
    ssh_key_id: String(profilePublic.ssh_key_id || server?.ssh_key_id || ""),
    description: target?.config?.description || server?.description || "",
    startup_input_after_connect: target?.config?.startup_input_after_connect || server?.startup_input_after_connect || "",
    force_shell_command: target?.config?.force_shell_command || server?.force_shell_command || "",
    setup_later: false,
  };
}

export function activeCredential({ credentials, form }) {
  return credentials.find((key) => Number(key.id) === Number(form.ssh_key_id)) || null;
}

export function syncForm({ form, firstCredentialID }) {
  if (form.connector_kind !== "ssh" || form.ssh_key_id || !firstCredentialID) return form;
  return { ...form, ssh_key_id: firstCredentialID };
}

export function submitDisabled({ state, credentials }) {
  return state.state === "saving" || credentials.length === 0;
}

export function submitLabel({ state, mode, form }) {
  if (state.state === "saving") return form.setup_later ? "Saving..." : "Testing...";
  return mode === "edit" ? "Save changes" : "Create connector";
}

export async function save({ mode, form, target }) {
  const payload = payloadFromForm(form);
  if (mode === "edit") {
    if (!target) throw new Error("SSH connector target is not loaded.");
    await saveFromPayload({ targetID: target.id, payload, setupLater: Boolean(form.setup_later), previousTarget: target });
    return;
  }
  await createFromPayload({ payload, setupLater: Boolean(form.setup_later) });
}

export async function deleteTarget({ target, removeKey }) {
  if (!target) throw new Error("SSH connector target is not loaded.");
  await apiDelete(`/api/connector-targets/${target.id}${removeKey ? "?remove_key=true" : ""}`);
}

export function emptyCredentialState() {
  return {
    mode: "generate",
    form: { ...emptySSHCredentialForm },
    importForm: { ...emptySSHCredentialImportForm },
  };
}

export async function loadCredentialResources() {
  const data = await apiGet("/api/connectors/ssh/credentials");
  return (data.items || data || []).map((item) => ({
    ...item,
    connector_kind: "ssh",
    resource_kind: "ssh_key",
    resource_ref: `ssh:ssh_key:${item.id}`,
  }));
}

export function credentialStateFromRow({ row }) {
  return {
    mode: "generate",
    form: { name: row.name, key_type: row.kind },
    importForm: { ...emptySSHCredentialImportForm },
  };
}

export function credentialFormProps({ formState, setFormState, formMode, state, onSubmit }) {
  const setMode = (nextMode) => {
    setFormState((current) => ({
      ...current,
      mode: nextMode,
      form: nextMode === "generate" ? current.form : { ...emptySSHCredentialForm },
      importForm: nextMode === "import" ? current.importForm : { ...emptySSHCredentialImportForm },
    }));
  };
  return {
    formMode,
    mode: formState.mode,
    form: formState.form,
    importForm: formState.importForm,
    state,
    onModeChange: setMode,
    onFormChange: (form) => setFormState((current) => ({ ...current, form })),
    onImportFormChange: (importForm) => setFormState((current) => ({ ...current, importForm })),
    onReadImportFile: async (event) => {
      const file = event.target.files?.[0];
      if (!file) return;
      const text = await file.text();
      setFormState((current) => ({
        ...current,
        importForm: {
          ...current.importForm,
          name: current.importForm.name === emptySSHCredentialImportForm.name ? keyNameFromFilename(file.name) : current.importForm.name,
          private_key: text,
        },
      }));
      event.target.value = "";
    },
    onCreate: (event) => onSubmit(event, "create"),
    onImport: (event) => onSubmit(event, "import"),
    onUpdate: (event) => onSubmit(event, "update"),
  };
}

export async function saveCredential({ operation, row, formState }) {
  if (operation === "create") {
    await apiPost("/api/connectors/ssh/credentials", formState.form);
    return { message: "SSH credential created." };
  }
  if (operation === "import") {
    await apiPost("/api/connectors/ssh/credentials/import", formState.importForm);
    return { message: "SSH credential imported." };
  }
  if (operation === "update") {
    if (!row) throw new Error("SSH credential is not loaded.");
    await apiPut(`/api/connectors/ssh/credentials/${row.id}`, { name: formState.form.name });
    return { message: "SSH credential updated." };
  }
  throw new Error("Unsupported SSH credential operation.");
}

export async function deleteCredential({ row }) {
  await apiDelete(`/api/connectors/ssh/credentials/${row.id}`);
}

export function credentialRows({ credentials, targets = [] }) {
  return credentials.map((key) => {
    const linkedTargets = targets.filter((target) =>
      (target.profiles || []).some((profile) => Number(profile.public?.ssh_key_id) === Number(key.id))
    );
    return {
      row_id: `ssh-key:${key.id}`,
      connector_kind: "ssh",
      resource_kind: key.resource_kind || "ssh_key",
      connector_label: "SSH",
      credential: key,
      id: key.id,
      name: key.name,
      kind: key.key_type,
      target_label: "Gateway key material",
      target_detail: linkedTargets.length > 0 ? linkedTargets.map((target) => target.name).join(", ") : "No linked SSH connectors",
      metadata: [key.fingerprint],
      delete_disabled: linkedTargets.length > 0 ? "Remove connector links first" : "",
    };
  });
}

export async function test({ target, profile }) {
  const selectedProfile = profile || (target?.profiles?.length === 1 ? target.profiles[0] : null);
  if (!target || !selectedProfile) throw new Error("SSH connector profile is not loaded.");
  const data = await apiPost(`/api/connector-targets/${target.id}/profiles/${selectedProfile.id}/test`, {});
  return { ok: data.ok, error: data.message || data.stderr || null, data };
}

export function canEdit({ target }) {
  return Boolean(target);
}

export function canDelete({ target }) {
  return Boolean(target);
}

export function credentialHint({ target, profile, credentials }) {
  const selectedProfile = profile || (target?.profiles?.length === 1 ? target.profiles[0] : null);
  const sshKeyID = selectedProfile?.public?.ssh_key_id;
  if (!sshKeyID) return null;
  const key = credentials.find((item) => Number(item.id) === Number(sshKeyID));
  return key ? `Key: ${key.name}` : `Key: #${sshKeyID}`;
}

export function targetEndpoint({ target, profile }) {
  const selectedProfile = profile || (target?.profiles?.length === 1 ? target.profiles[0] : null);
  const username = selectedProfile?.public?.username || "ssh";
  const host = target.config?.host || "host";
  const port = target.config?.port || 22;
  return `${username}@${host}:${port}`;
}

export function targetDisplayName({ target }) {
  return target?.target_name || target?.name || "SSH target";
}

export function targetSubtitle({ target, runtimeTarget }) {
  const username = target?.public?.username || runtimeTarget?.username || "ssh";
  const host = target?.config?.host || runtimeTarget?.host || "host";
  const port = target?.config?.port || runtimeTarget?.port || 22;
  return `${username}@${host}:${port}`;
}

export function targetProfileLabel({ target } = {}) {
  return target?.profile_label || target?.public?.username || "terminal";
}

export function usesLiveConsole() {
  return true;
}

export function liveConsoleRuntimeTarget({ target }) {
  const profile = target.public || {};
  return {
    id: target.runtime_id,
    name: targetDisplayName({ target }),
    host: target.config?.host || "",
    port: target.config?.port || 0,
    username: profile.username || "",
    ssh_key_id: profile.ssh_key_id || 0,
    description: target.config?.description || "",
    startup_input_after_connect: target.config?.startup_input_after_connect || "",
    force_shell_command: target.config?.force_shell_command || "",
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
    description: "Remove this SSH connector from aipermission. You can also remove the selected gateway public key from remote authorized_keys first.",
    details: [
      { label: "Connector", value: target?.name },
      { label: "Reference", value: target ? `${target.connector_kind}:${target.id}` : "" },
      { label: "Profiles", value: target?.profiles?.length ? `${target.profiles.length} credential profile${target.profiles.length === 1 ? "" : "s"}` : "" },
    ],
    notice:
      "Remote key cleanup connects to the target, removes entries containing the selected gateway public key blob from ~/.ssh/authorized_keys, then deletes the local connector record.",
    actions: [
      { label: "Delete local only", removeKey: false, variant: "outline" },
      { label: "Remove key and delete", pendingLabel: "Deleting...", removeKey: true },
    ],
  };
}

export function hostKeyActionFromError(error, { mode, form, target, profile, testKey, operation, container }) {
  if (!isHostKeyError(error)) return null;
  if (operation === "test") {
    return { kind: "ssh", type: "test", target, profile, testKey };
  }
  if (operation === "docker-check") {
    return { kind: "ssh", type: "docker-check", target, profile };
  }
  if (operation === "docker-logs") {
    return { kind: "ssh", type: "docker-logs", target, profile, container };
  }
  return {
    kind: "ssh",
    type: mode === "edit" ? "save" : "create",
    payload: payloadFromForm(form),
    target,
    setupLater: Boolean(form.setup_later),
  };
}

export function operationFromError(error, context) {
  const action = hostKeyActionFromError(error, context);
  if (!action) return null;
  return {
    open: true,
    connector_kind: action.kind,
    type: "host-key",
    hostKey: error.data.host_key,
    action,
    state: "idle",
    error: null,
  };
}

export async function resumeHostKeyAction(action) {
  if (action.type === "create") {
    await createFromPayload({ payload: action.payload, setupLater: Boolean(action.setupLater) });
    return { message: "Connector created." };
  }
  if (action.type === "save") {
    if (!action.target) throw new Error("SSH connector target is not loaded.");
    await saveFromPayload({ targetID: action.target.id, payload: action.payload, setupLater: Boolean(action.setupLater), previousTarget: action.target });
    return { message: "Connector updated." };
  }
  if (action.type === "test") {
    const profile = action.profile || (action.target?.profiles?.length === 1 ? action.target.profiles[0] : null);
    if (!action.target || !profile) throw new Error("SSH connector profile is not loaded.");
    const data = await apiPost(`/api/connector-targets/${action.target.id}/profiles/${profile.id}/test`, {});
    return { testKey: action.testKey, test: { ok: data.ok, error: data.stderr || null, data } };
  }
  throw new Error("Unsupported SSH host-key action.");
}

async function createFromPayload({ payload, setupLater }) {
  if (!setupLater) {
    const testResult = await apiPost("/api/connector-targets/test", {
      connector_kind: "ssh",
      name: payload.name,
      config: targetConfigFromPayload(payload),
      profile: profilePublicFromPayload(payload),
    });
    if (!testResult.ok) {
      throw new Error(testResult.stderr || testResult.stdout || "SSH connection test failed. Paste the install command on the server first, or choose setup later.");
    }
  }
  await createTargetWithProfile({
    targetPayload: {
      connector_kind: "ssh",
      name: payload.name,
      config: targetConfigFromPayload(payload),
    },
    profilePayload: {
      kind: "private_key",
      label: payload.username,
      public: profilePublicFromPayload(payload),
    },
  });
}

async function saveFromPayload({ targetID, payload, setupLater, previousTarget }) {
  if (!setupLater) {
    const testResult = await apiPost("/api/connector-targets/test", {
      connector_kind: "ssh",
      name: payload.name,
      config: targetConfigFromPayload(payload),
      profile: profilePublicFromPayload(payload),
    });
    if (!testResult.ok) {
      throw new Error(testResult.stderr || testResult.stdout || "SSH connection test failed. Paste the install command on the target first, or choose setup later.");
    }
  }
  const profile = previousTarget?.profiles?.find((item) => Number(item.id) === Number(payload.profile_id)) || (previousTarget?.profiles?.length === 1 ? previousTarget.profiles[0] : null);
  if (!profile) throw new Error("SSH connector profile is not loaded.");
  await updateTargetWithProfile({
    targetID,
    previousTarget,
    profileID: profile.id,
    targetPayload: {
      name: payload.name,
      config: targetConfigFromPayload(payload),
    },
    profilePayload: {
      kind: profile.kind || "private_key",
      label: payload.username,
      public: profilePublicFromPayload(payload),
    },
  });
}

export async function checkDocker({ target, profile }) {
  if (!target || !profile) throw new Error("SSH connector target profile is not loaded.");
  return apiPost(`/api/connector-targets/${target.id}/operations/docker-check`, { profile_id: Number(profile.id) });
}

export async function readDockerLogs({ target, profile, container, tail = 300 }) {
  if (!target || !profile || !container) throw new Error("SSH connector target profile is not loaded.");
  return apiPost(`/api/connector-targets/${target.id}/operations/docker-logs`, { profile_id: Number(profile.id), container_ref: container.id || container.name, tail: Number(tail) || 300 });
}

function payloadFromForm(form) {
  return {
    name: form.name,
    host: form.host,
    port: Number(form.port),
    username: form.username,
    ssh_key_id: Number(form.ssh_key_id),
    profile_id: Number(form.profile_id || 0),
    description: form.description,
    startup_input_after_connect: form.startup_input_after_connect,
    force_shell_command: form.force_shell_command,
  };
}

function targetConfigFromPayload(payload) {
  return {
    host: payload.host,
    port: payload.port,
    description: payload.description,
    startup_input_after_connect: payload.startup_input_after_connect,
    force_shell_command: payload.force_shell_command,
  };
}

function profilePublicFromPayload(payload) {
  return {
    username: payload.username,
    ssh_key_id: payload.ssh_key_id,
  };
}

function isHostKeyError(error) {
  return error.status === 409 && ["unknown_ssh_host_key", "changed_ssh_host_key"].includes(error.data?.code) && Boolean(error.data?.host_key);
}

function keyNameFromFilename(filename) {
  return filename
    .replace(/\.[^.]+$/, "")
    .replace(/[^a-zA-Z0-9_. -]+/g, "-")
    .trim()
    .slice(0, 80) || emptySSHCredentialImportForm.name;
}
