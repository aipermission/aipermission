import { apiDelete, apiPost, apiPut } from "../../../lib/api.js";
import { createTargetWithProfile, updateTargetWithProfile } from "../target-profile-save.js";

export function createDatabaseConnectorModel(config) {
  const {
    kind,
    label,
    targetDefaults,
    credentialDefaults,
    defaultRiskLabel,
    targetForm,
    targetConfig,
    targetEndpoint,
    credentialExtras = () => ({}),
    credentialPublic = (form) => ({ username: form.username }),
    credentialMetadata = defaultCredentialMetadata,
    includeEmptyPassword = false,
  } = config;

  function emptyForm() {
    return {
      connector_kind: kind,
      ...targetDefaults,
      profile_label: "readonly",
      username: "",
      password: "",
      risk_label: defaultRiskLabel,
    };
  }

  function formFromTarget({ target, profile }) {
    const selectedProfile = profile || (target?.profiles?.length === 1 ? target.profiles[0] : {});
    return {
      connector_kind: kind,
      profile_id: selectedProfile.id ? String(selectedProfile.id) : "",
      name: target.name || "",
      ...targetForm(target),
      profile_label: selectedProfile.label || "readonly",
      username: selectedProfile.public?.username || "",
      password: "",
      risk_label: selectedProfile.risk_label || defaultRiskLabel,
    };
  }

  function activeCredential() {
    return null;
  }

  function syncForm({ form }) {
    if (form.connector_kind !== kind) return form;
    const next = { ...form };
    if (next.connection_mode === "direct") next.transport_target_ref = "";
    if (next.connection_mode === "over_ssh" && !next.host) next.host = "127.0.0.1";
    return next;
  }

  function submitDisabled({ state }) {
    return state.state === "saving";
  }

  function submitLabel({ state, mode }) {
    if (state.state === "saving") return "Saving...";
    return mode === "edit" ? "Save changes" : "Create connector";
  }

  async function save({ mode, form, target }) {
    if (mode === "edit") {
      await updateTarget(form, target);
      return;
    }
    await createTarget(form);
  }

  async function deleteTarget({ target }) {
    await apiDelete(`/api/connector-targets/${target.id}`);
  }

  function emptyCredentialState({ targets = [] } = {}) {
    const firstTarget = targets.find((target) => target.connector_kind === kind);
    return { form: { ...credentialDefaults, target_id: String(firstTarget?.id || "") } };
  }

  function credentialStateFromRow({ row }) {
    return {
      form: {
        ...credentialDefaults,
        target_id: String(row.target_id || ""),
        profile_label: row.name,
        username: row.profile?.public?.username || "",
        password: "",
        risk_label: row.profile?.risk_label || "",
        ...credentialExtras(row),
      },
    };
  }

  function credentialFormProps({ targets, formState, setFormState, formMode, state, onSubmit }) {
    return {
      form: formState.form,
      formMode,
      targets,
      state,
      onChange: (form) => setFormState({ form }),
      onSubmit: (event) => onSubmit(event, formMode === "edit" ? "update" : "create"),
    };
  }

  async function saveCredential({ operation, row, formState }) {
    const form = formState.form;
    if (operation === "create") {
      await apiPost(`/api/connector-targets/${form.target_id}/profiles`, profilePayload(form, null, true));
      return { message: `${label} credential created.` };
    }
    if (operation === "update") {
      if (!row) throw new Error(`${label} credential is not loaded.`);
      await apiPut(`/api/connector-targets/${form.target_id}/profiles/${row.id}`, profilePayload(form, row.profile, false));
      return { message: `${label} credential updated.` };
    }
    throw new Error(`Unsupported ${label} credential operation.`);
  }

  async function deleteCredential({ row }) {
    await apiDelete(`/api/connector-targets/${row.target_id}/profiles/${row.id}`);
  }

  function credentialRows({ targets }) {
    return targets.flatMap((target) =>
      (target.profiles || [])
        .filter(() => target.connector_kind === kind)
        .map((profile) => ({
          row_id: `${target.connector_kind}:${target.id}:${profile.id}`,
          connector_kind: target.connector_kind,
          resource_kind: "credential_profile",
          connector_label: label,
          id: profile.id,
          target_id: target.id,
          name: profile.label,
          kind: profile.kind,
          profile,
          target_label: target.name,
          target_detail: targetEndpoint({ target }),
          metadata: credentialMetadata(profile),
          delete_disabled: "",
        })),
    );
  }

  async function test({ target, profile }) {
    const selectedProfile = profile || (target?.profiles?.length === 1 ? target.profiles[0] : null);
    if (!selectedProfile) throw new Error("Connector profile is not loaded.");
    const data = await apiPost(`/api/connector-targets/${target.id}/profiles/${selectedProfile.id}/test`, {});
    return { ok: data.ok, error: data.message || null, data };
  }

  function targetDisplayName({ target }) {
    if (!target) return `${label} target`;
    return target.target_name || target.name || `${label} target`;
  }

  function targetSubtitle({ target }) {
    return targetEndpoint({ target });
  }

  function targetProfileLabel({ target }) {
    return target?.profile_label || "default";
  }

  function deleteDialog({ target }) {
    return {
      title: target ? `Delete ${target.name}` : "Delete connector",
      description: `Remove this ${label} connector target, credential profiles, and token action permissions from aipermission.`,
      details: [
        { label: "Connector", value: target?.name },
        { label: "Reference", value: target ? `${target.connector_kind}:${target.id}` : "" },
      ],
      notice: `This removes the connector target and its credential profiles. It does not change the external ${label} service.`,
      actions: [
        { label: "Cancel", action: "close", variant: "outline" },
        { label: "Delete connector", pendingLabel: "Deleting...", removeKey: false },
      ],
    };
  }

  async function createTarget(form) {
    await createTargetWithProfile({
      projectID: form.project_id,
      targetPayload: { connector_kind: kind, name: form.name, config: targetConfig(form) },
      profilePayload: profilePayload(form, null, true),
    });
  }

  async function updateTarget(form, target) {
    const profile =
      target?.profiles?.find((item) => Number(item.id) === Number(form.profile_id)) ||
      (target?.profiles?.length === 1 ? target.profiles[0] : null);
    if (!target || !profile) throw new Error(`${label} connector profile is not loaded.`);
    await updateTargetWithProfile({
      projectID: form.project_id,
      targetID: target.id,
      previousTarget: target,
      profileID: profile.id,
      targetPayload: { name: form.name, config: targetConfig(form) },
      profilePayload: profilePayload(form, profile, false),
    });
  }

  function profilePayload(form, profile, creating) {
    const payload = {
      kind: profile?.kind || "username_password",
      label: form.profile_label,
      public: credentialPublic(form),
      risk_label: form.risk_label || defaultRiskLabel,
    };
    if (form.password || (creating && includeEmptyPassword)) {
      payload.secret = { password: form.password };
    } else if (creating) {
      payload.secret = {};
    }
    return payload;
  }

  return {
    emptyForm,
    formFromTarget,
    activeCredential,
    syncForm,
    submitDisabled,
    submitLabel,
    save,
    deleteTarget,
    emptyCredentialState,
    credentialStateFromRow,
    credentialFormProps,
    saveCredential,
    deleteCredential,
    credentialRows,
    test,
    canEdit: () => true,
    canDelete: () => true,
    credentialHint: () => null,
    targetEndpoint,
    targetDisplayName,
    targetSubtitle,
    targetProfileLabel,
    usesLiveConsole: () => false,
    recoverableRunningActions: () => [],
    deleteDialog,
    operationFromError: () => null,
  };
}

function defaultCredentialMetadata(profile) {
  const items = [];
  if (profile.public?.username) items.push(`username: ${profile.public.username}`);
  if (profile.risk_label) items.push(`risk: ${profile.risk_label}`);
  if (items.length === 0) items.push("No public metadata");
  return items;
}
