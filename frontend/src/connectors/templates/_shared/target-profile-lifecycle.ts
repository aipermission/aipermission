import { connectorConnectionTestResponse } from "../../../lib/gateway-contracts/connector-management-contracts.ts";
import { apiDelete, apiPost, apiPut } from "../../../lib/api.ts";
import { createTargetWithProfile, updateTargetWithProfile } from "../target-profile-save.ts";
import type { FormEvent, SetStateAction } from "react";
import type {
  CredentialFormPropsContext,
  LifecycleCredentialFormProps,
  LifecycleCredentialContext,
  LifecycleCredentialForm,
  LifecycleCredentialRow,
  LifecycleMessage,
  LifecycleMessageContext,
  LifecycleOptions,
  LifecycleProfile,
  LifecycleSaveContext,
  LifecycleTarget,
  LifecycleTargetForm,
} from "./target-profile-lifecycle-types";

const lifecycleFunctions = new WeakSet();
const standardLifecycleFunctions = ["credentialFormProps", "deleteCredential", "deleteTarget", "save", "saveCredential", "test"] as const;

export function defaultTargetProfile<Profile extends LifecycleProfile>(
  target: LifecycleTarget<Profile> | null | undefined,
  profile: Profile | null | undefined,
  fallback: Partial<Profile> = {},
): Partial<Profile> {
  if (profile) return profile;
  return target?.profiles?.length === 1 ? target.profiles[0] : fallback;
}

export function standardSubmitLabel({ state, mode }: { state: { state: string }; mode: string }) {
  if (state.state === "saving") return "Saving...";
  return mode === "edit" ? "Save changes" : "Create connector";
}

export function firstTargetCredentialForm<Form>(targets: LifecycleTarget[], connectorKind: string, defaults: Form) {
  const firstTarget = targets.find((target) => target.connector_kind === connectorKind);
  return { form: { ...defaults, target_id: String(firstTarget?.id || "") } };
}

export function usernameCredentialStateFromRow(row: {
  target_id?: string | number;
  name: string;
  profile?: LifecycleProfile & { public?: { username?: string } };
}) {
  return {
    form: {
      target_id: String(row.target_id || ""),
      profile_label: row.name,
      username: row.profile?.public?.username || "",
      password: "",
      risk_label: row.profile?.risk_label || "",
    },
  };
}

export function createTargetProfileLifecycle<
  Form extends LifecycleTargetForm = LifecycleTargetForm,
  CredentialForm extends LifecycleCredentialForm = LifecycleCredentialForm,
  Profile extends LifecycleProfile = LifecycleProfile,
  Target extends LifecycleTarget<Profile> = LifecycleTarget<Profile>,
>({
  connectorKind,
  connectorLabel,
  targetPayload,
  profilePayload,
  credentialCreatedMessage = `${connectorLabel} credential created.`,
  credentialUpdatedMessage = `${connectorLabel} credential updated.`,
  credentialMissingMessage = `${connectorLabel} credential is not loaded.`,
  unsupportedCredentialMessage = `Unsupported ${connectorLabel} credential operation.`,
  beforeSave = null,
  beforeSaveCredential = null,
}: LifecycleOptions<Form, CredentialForm, Profile, Target>) {
  function selectedProfile(target: Target | null | undefined, profileID: string | number | undefined) {
    return (
      target?.profiles?.find((item) => Number(item.id) === Number(profileID)) ||
      (target?.profiles?.length === 1 ? target.profiles[0] : null)
    );
  }

  async function save({ mode, form, target }: LifecycleSaveContext<Form, Target>) {
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
    if (!target?.id || !profile?.id) throw new Error(`${connectorLabel} connector profile is not loaded.`);
    await updateTargetWithProfile({
      projectID: form.project_id,
      targetID: target.id,
      profileID: profile.id,
      targetPayload: targetPayload(form),
      profilePayload: profilePayload(form, { operation: "target-update", profile }),
    });
  }

  async function deleteTarget({ target }: { target: Target }) {
    await apiDelete(`/api/connector-targets/${target.id}`);
  }

  function credentialFormProps<FormState extends { form: object }, Status>({
    targets,
    formState,
    setFormState,
    formMode,
    state,
    onSubmit,
  }: CredentialFormPropsContext<FormState["form"], FormState, Status, Target>): LifecycleCredentialFormProps<
    FormState["form"],
    Status,
    Target
  > {
    return {
      form: formState.form,
      formMode,
      targets,
      state,
      onChange: (form: SetStateAction<FormState["form"]>) =>
        setFormState((current) => ({
          ...current,
          form: typeof form === "function" ? form(current.form) : form,
        })),
      onSubmit: (event: FormEvent<HTMLFormElement>) => onSubmit(event, formMode === "edit" ? "update" : "create"),
    };
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
        profilePayload(form, { operation: "credential-create", profile: null }),
      );
      return { message: lifecycleMessage(credentialCreatedMessage, { form, row: null, target }) };
    }
    if (operation === "update") {
      if (!row) throw new Error(credentialMissingMessage);
      await apiPut(
        `/api/connector-targets/${form.target_id}/profiles/${row.id}`,
        profilePayload(form, { operation: "credential-update", profile: row.profile || null }),
      );
      return { message: lifecycleMessage(credentialUpdatedMessage, { form, row, target }) };
    }
    throw new Error(unsupportedCredentialMessage);
  }

  async function deleteCredential({ row }: { row: LifecycleCredentialRow<Profile, Target> }) {
    await apiDelete(`/api/connector-targets/${row.target_id}/profiles/${row.id}`);
  }

  async function test({ target, profile }: { target: Target; profile?: Profile | null }) {
    const selected = profile || selectedProfile(target, "");
    if (!selected) throw new Error(`${connectorLabel} connector profile is not loaded.`);
    const data = connectorConnectionTestResponse(await apiPost(`/api/connector-targets/${target.id}/profiles/${selected.id}/test`, {}));
    return { ok: data.ok, error: data.message || null, data };
  }

  const lifecycle = { credentialFormProps, deleteCredential, deleteTarget, save, saveCredential, test };
  standardLifecycleFunctions.forEach((name) => lifecycleFunctions.add(lifecycle[name]));
  return lifecycle;
}

export function usesStandardTargetProfileLifecycle(model: unknown) {
  if (!model || (typeof model !== "object" && typeof model !== "function")) return false;
  const candidate = model as Record<string, unknown>;
  return standardLifecycleFunctions.every((name) => typeof candidate[name] === "function" && lifecycleFunctions.has(candidate[name]));
}

export function connectorCredentialRows<Profile extends LifecycleProfile, Target extends LifecycleTarget<Profile>, Metadata>({
  targets,
  connectorKind,
  connectorLabel,
  targetEndpoint,
  credentialMetadata,
  includeTarget = false,
}: {
  targets: Target[];
  connectorKind: string;
  connectorLabel: string | ((_target: Target) => string);
  targetEndpoint: (_context: { target: Target }) => string;
  credentialMetadata: (_profile: Profile) => Metadata;
  includeTarget?: boolean;
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

function lifecycleMessage<Form, Profile extends LifecycleProfile, Target extends LifecycleTarget<Profile>>(
  value: LifecycleMessage<Form, Profile, Target>,
  context: LifecycleMessageContext<Form, Profile, Target>,
) {
  return typeof value === "function" ? value(context) : value;
}
