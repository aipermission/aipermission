import { apiDelete, apiPost, apiPut } from "../../lib/api.ts";
import { createTargetWithProfile, updateTargetWithProfile } from "./atomic-save.ts";
import type {
  LifecycleCredentialContext,
  LifecycleCredentialForm,
  LifecycleCredentialRow,
  LifecycleMessage,
  LifecycleMessageContext,
  LifecycleProfile,
  LifecycleSaveContext,
  LifecycleTarget,
  LifecycleTargetForm,
  ProfilePersistenceOptions,
} from "./types";

export function selectedTargetProfile<Profile extends LifecycleProfile>(
  target: LifecycleTarget<Profile> | null | undefined,
  profileID: string | number | undefined,
): Profile | null {
  return (
    target?.profiles?.find((item) => Number(item.id) === Number(profileID)) || (target?.profiles?.length === 1 ? target.profiles[0] : null)
  );
}

export function createProfilePersistence<
  Form extends LifecycleTargetForm,
  CredentialForm extends LifecycleCredentialForm,
  Profile extends LifecycleProfile,
  Target extends LifecycleTarget<Profile>,
>({
  connectorKind,
  connectorLabel,
  targetPayload,
  targetProfilePayload,
  credentialProfilePayload,
  credentialCreatedMessage = `${connectorLabel} credential created.`,
  credentialUpdatedMessage = `${connectorLabel} credential updated.`,
  credentialMissingMessage = `${connectorLabel} credential is not loaded.`,
  unsupportedCredentialMessage = `Unsupported ${connectorLabel} credential operation.`,
  invalidIdentityMessage = `${connectorLabel} connector profile is not loaded.`,
  beforeSave,
  beforeSaveCredential,
}: ProfilePersistenceOptions<Form, CredentialForm, Profile, Target>) {
  async function save({ mode, form, target }: LifecycleSaveContext<Form, Target>) {
    await beforeSave?.({ mode, form, target });
    if (mode !== "edit") {
      await createTargetWithProfile({
        projectID: form.project_id,
        targetPayload: { connector_kind: connectorKind, ...targetPayload(form) },
        profilePayload: targetProfilePayload(form, { operation: "target-create", profile: null }),
      });
      return;
    }
    const profile = selectedTargetProfile(target, form.profile_id);
    if (!target || !profile) throw new Error(`${connectorLabel} connector profile is not loaded.`);
    if (!target.id || !profile.id) throw new Error(invalidIdentityMessage);
    await updateTargetWithProfile({
      projectID: form.project_id,
      targetID: target.id,
      profileID: profile.id,
      targetPayload: targetPayload(form),
      profilePayload: targetProfilePayload(form, { operation: "target-update", profile }),
    });
  }

  async function deleteTarget({ target }: { target: Target }) {
    await apiDelete(`/api/connector-targets/${target.id}`);
  }

  async function saveCredential({
    operation,
    row,
    formState,
    targets = [],
  }: Omit<LifecycleCredentialContext<CredentialForm, Profile, Target>, "form" | "targets"> & {
    formState: { form: CredentialForm };
    targets?: Target[];
  }) {
    const form = formState.form;
    await beforeSaveCredential?.({ operation, row, form, targets });
    const target = row?.target || targets.find((item) => Number(item.id) === Number(form.target_id)) || null;
    if (operation === "create") {
      await apiPost(
        `/api/connector-targets/${form.target_id}/profiles`,
        credentialProfilePayload(form, { operation: "credential-create", profile: null }),
      );
      return { message: lifecycleMessage(credentialCreatedMessage, { form, row: null, target }) };
    }
    if (operation === "update") {
      if (!row) throw new Error(credentialMissingMessage);
      await apiPut(
        `/api/connector-targets/${form.target_id}/profiles/${row.id}`,
        credentialProfilePayload(form, { operation: "credential-update", profile: row.profile || null }),
      );
      return { message: lifecycleMessage(credentialUpdatedMessage, { form, row, target }) };
    }
    throw new Error(unsupportedCredentialMessage);
  }

  async function deleteCredential({ row }: { row: LifecycleCredentialRow<Profile, Target> }) {
    await apiDelete(`/api/connector-targets/${row.target_id}/profiles/${row.id}`);
  }

  return { save, deleteTarget, saveCredential, deleteCredential };
}

function lifecycleMessage<Form, Profile extends LifecycleProfile, Target extends LifecycleTarget<Profile>>(
  value: LifecycleMessage<Form, Profile, Target>,
  context: LifecycleMessageContext<Form, Profile, Target>,
) {
  return typeof value === "function" ? value(context) : value;
}
