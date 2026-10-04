import { connectorConnectionTestResponse } from "../../../lib/gateway-contracts/connector-management-contracts.ts";
import { apiPost } from "../../../lib/api.ts";
import { createProfilePersistence, selectedTargetProfile } from "../../profile-lifecycle/persistence.ts";
import type { FormEvent, SetStateAction } from "react";
import type {
  CredentialFormPropsContext,
  LifecycleCredentialFormProps,
  LifecycleCredentialForm,
  LifecycleDisplayRow,
  LifecycleOptions,
  LifecycleProfile,
  LifecycleTarget,
  LifecycleTargetForm,
} from "../../profile-lifecycle/types";

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
>(options: LifecycleOptions<Form, CredentialForm, Profile, Target>) {
  const { profilePayload, ...persistenceOptions } = options;
  const { connectorLabel } = persistenceOptions;
  const { save, deleteTarget, saveCredential, deleteCredential } = createProfilePersistence({
    ...persistenceOptions,
    targetProfilePayload: profilePayload,
    credentialProfilePayload: profilePayload,
  });

  function credentialFormProps<FormState extends { form: object }, Status, FormTarget extends Target>({
    targets,
    formState,
    setFormState,
    formMode,
    state,
    onSubmit,
  }: CredentialFormPropsContext<FormState["form"], FormState, Status, FormTarget>): LifecycleCredentialFormProps<
    FormState["form"],
    Status,
    FormTarget
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

  async function test({ target, profile }: { target: Target; profile?: Profile | null }) {
    const selected = profile || selectedTargetProfile(target, "");
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
}): LifecycleDisplayRow<Profile, Target, Metadata>[] {
  return targets
    .filter((target) => target.connector_kind === connectorKind)
    .flatMap((target) =>
      (target.profiles || []).map((profile) => ({
        row_id: `${target.connector_kind}:${target.id}:${profile.id}`,
        connector_kind: connectorKind,
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
