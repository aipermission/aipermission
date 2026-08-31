import { apiDelete, apiPost, apiPut } from "../../../lib/api.js";
import { createTargetWithProfile, updateTargetWithProfile } from "../target-profile-save.js";

export function createTargetProfileLifecycle({
  connectorKind,
  connectorLabel,
  targetPayload,
  profilePayload,
  credentialCreatedMessage = `${connectorLabel} credential created.`,
  credentialUpdatedMessage = `${connectorLabel} credential updated.`,
  credentialMissingMessage = `${connectorLabel} credential is not loaded.`,
  unsupportedCredentialMessage = `Unsupported ${connectorLabel} credential operation.`,
  beforeSave,
  beforeSaveCredential,
}) {
  function selectedProfile(target, profileID) {
    return (
      target?.profiles?.find((item) => Number(item.id) === Number(profileID)) ||
      (target?.profiles?.length === 1 ? target.profiles[0] : null)
    );
  }

  async function save({ mode, form, target }) {
    await beforeSave?.({ mode, form, target });
    if (mode !== "edit") {
      await createTargetWithProfile({
        projectID: form.project_id,
        targetPayload: { connector_kind: connectorKind, ...targetPayload(form) },
        profilePayload: profilePayload(form, { operation: "target-create", profile: null }),
      });
      return;
    }
    const profile = selectedProfile(target, form.profile_id);
    if (!target || !profile) throw new Error(`${connectorLabel} connector profile is not loaded.`);
    await updateTargetWithProfile({
      projectID: form.project_id,
      targetID: target.id,
      profileID: profile.id,
      targetPayload: targetPayload(form),
      profilePayload: profilePayload(form, { operation: "target-update", profile }),
    });
  }

  async function deleteTarget({ target }) {
    await apiDelete(`/api/connector-targets/${target.id}`);
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

  async function saveCredential({ operation, row, formState, targets = [] }) {
    const form = formState.form;
    await beforeSaveCredential?.({ operation, row, form, targets });
    const target = row?.target || targets.find((item) => Number(item.id) === Number(form.target_id)) || null;
    if (operation === "create") {
      await apiPost(
        `/api/connector-targets/${form.target_id}/profiles`,
        profilePayload(form, { operation: "credential-create", profile: null }),
      );
      return { message: lifecycleMessage(credentialCreatedMessage, { form, row: null, target }) };
    }
    if (operation === "update") {
      if (!row) throw new Error(credentialMissingMessage);
      await apiPut(
        `/api/connector-targets/${form.target_id}/profiles/${row.id}`,
        profilePayload(form, { operation: "credential-update", profile: row.profile }),
      );
      return { message: lifecycleMessage(credentialUpdatedMessage, { form, row, target }) };
    }
    throw new Error(unsupportedCredentialMessage);
  }

  async function deleteCredential({ row }) {
    await apiDelete(`/api/connector-targets/${row.target_id}/profiles/${row.id}`);
  }

  async function test({ target, profile }) {
    const selected = profile || selectedProfile(target, "");
    if (!selected) throw new Error(`${connectorLabel} connector profile is not loaded.`);
    const data = await apiPost(`/api/connector-targets/${target.id}/profiles/${selected.id}/test`, {});
    return { ok: data.ok, error: data.message || null, data };
  }

  return { credentialFormProps, deleteCredential, deleteTarget, save, saveCredential, test };
}

export function connectorCredentialRows({
  targets,
  connectorKind,
  connectorLabel,
  targetEndpoint,
  credentialMetadata,
  includeTarget = false,
}) {
  return targets
    .filter((target) => target.connector_kind === connectorKind)
    .flatMap((target) =>
      (target.profiles || []).map((profile) => ({
        row_id: `${target.connector_kind}:${target.id}:${profile.id}`,
        connector_kind: target.connector_kind,
        resource_kind: "credential_profile",
        connector_label: typeof connectorLabel === "function" ? connectorLabel(target) : connectorLabel,
        id: profile.id,
        target_id: target.id,
        name: profile.label,
        kind: profile.kind,
        profile,
        ...(includeTarget ? { target } : {}),
        target_label: target.name,
        target_detail: targetEndpoint({ target }),
        metadata: credentialMetadata(profile),
        delete_disabled: "",
      })),
    );
}

function lifecycleMessage(value, context) {
  return typeof value === "function" ? value(context) : value;
}
