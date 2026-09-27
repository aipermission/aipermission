import { connectorConnectionTestResponse } from "../../../lib/gateway-contracts/connector-management-contracts.ts";
import { apiDelete, apiPost, apiPut } from "../../../lib/api.ts";
import { createTargetWithProfile, updateTargetWithProfile } from "../target-profile-save.ts";
import { connectorCredentialRows } from "./target-profile-lifecycle.ts";
import type { DatabaseCredentialForm } from "./database-form-types";
import type { ConnectorDeleteDialog } from "../../editor/connector-editor-dialog-types";
import type {
  CredentialFormArguments,
  DatabaseCredentialRow,
  DatabaseModelConfig,
  DatabaseModelForm,
  DatabasePresentationTarget,
  DatabaseProfile,
  DatabaseTarget,
  DatabaseTargetDefaults,
  DatabaseTransportForm,
  SyncedDatabaseForm,
} from "./database-model-types";

export function createDatabaseConnectorModel<Fields extends DatabaseTargetDefaults, Credential extends DatabaseCredentialForm>(
  config: DatabaseModelConfig<Fields, Credential>,
) {
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
    credentialPublic = defaultCredentialPublic,
    targetCredentialPublic = defaultCredentialPublic,
    credentialMetadata = defaultCredentialMetadata,
    includeEmptyPassword = false,
  } = config;

  function emptyForm(): DatabaseModelForm<Fields> {
    return {
      connector_kind: kind,
      ...targetDefaults,
      profile_label: "readonly",
      username: "",
      password: "",
      risk_label: defaultRiskLabel,
    };
  }

  function formFromTarget({ target, profile }: { target: DatabaseTarget; profile?: DatabaseProfile | null }) {
    const selectedProfile: Partial<DatabaseProfile> = profile || (target?.profiles?.length === 1 ? target.profiles[0] : {});
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

  function syncForm<Form extends DatabaseTransportForm>({ form }: { form: Form }): SyncedDatabaseForm<Form> {
    if (form.connector_kind !== kind) return form;
    const next = { ...form };
    if (next.connection_mode === "direct") next.transport_target_ref = "";
    if (next.connection_mode === "over_ssh" && !next.host) next.host = "127.0.0.1";
    return next;
  }

  function syncEditorForm({ form }: { form: DatabaseModelForm<Fields> }): DatabaseModelForm<Fields> {
    const next = syncForm({ form });
    return { ...form, host: next.host ?? form.host, transport_target_ref: next.transport_target_ref ?? form.transport_target_ref };
  }

  function submitDisabled({ state }: { state: { state: string } }): boolean {
    return state.state === "saving";
  }

  function submitLabel({ state, mode }: { state: { state: string }; mode: string }): string {
    if (state.state === "saving") return "Saving...";
    return mode === "edit" ? "Save changes" : "Create connector";
  }

  async function save({
    mode,
    form,
    target,
  }: {
    mode: string;
    form: DatabaseModelForm<Fields>;
    target?: DatabaseTarget | null;
  }): Promise<void> {
    if (mode === "edit") {
      await updateTarget(form, target);
      return;
    }
    await createTarget(form);
  }

  async function deleteTarget({ target }: { target: DatabaseTarget }): Promise<void> {
    await apiDelete(`/api/connector-targets/${target.id}`);
  }

  function emptyCredentialState({ targets = [] }: { targets?: DatabaseTarget[] } = {}) {
    const firstTarget = targets.find((target) => target.connector_kind === kind);
    return { form: { ...credentialDefaults, target_id: String(firstTarget?.id || "") } };
  }

  function credentialStateFromRow({ row }: { row: DatabaseCredentialRow }) {
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

  function credentialFormProps({ targets, formState, setFormState, formMode, state, onSubmit }: CredentialFormArguments<Credential>) {
    return {
      form: formState.form,
      formMode,
      targets,
      state,
      onChange: (form: Credential) => setFormState({ form }),
      onSubmit: (event: Parameters<CredentialFormArguments<Credential>["onSubmit"]>[0]) =>
        onSubmit(event, formMode === "edit" ? "update" : "create"),
    };
  }

  async function saveCredential({
    operation,
    row,
    formState,
  }: {
    operation: string;
    row?: DatabaseCredentialRow | null;
    formState: { form: Credential };
  }) {
    const form = formState.form;
    if (operation === "create") {
      await apiPost(`/api/connector-targets/${form.target_id}/profiles`, profilePayload(form, null, true, credentialPublic(form)));
      return { message: `${label} credential created.` };
    }
    if (operation === "update") {
      if (!row) throw new Error(`${label} credential is not loaded.`);
      await apiPut(
        `/api/connector-targets/${form.target_id}/profiles/${row.id}`,
        profilePayload(form, row.profile ?? null, false, credentialPublic(form)),
      );
      return { message: `${label} credential updated.` };
    }
    throw new Error(`Unsupported ${label} credential operation.`);
  }

  async function deleteCredential({ row }: { row: Pick<DatabaseCredentialRow, "id" | "target_id"> }): Promise<void> {
    await apiDelete(`/api/connector-targets/${row.target_id}/profiles/${row.id}`);
  }

  function credentialRows<Profile extends DatabaseProfile, Target extends DatabaseTarget & { profiles?: Profile[] }>({
    targets,
  }: {
    targets: Target[];
  }) {
    return connectorCredentialRows<Profile, Target, string[]>({
      targets,
      connectorKind: kind,
      connectorLabel: label,
      targetEndpoint,
      credentialMetadata,
    });
  }

  async function test({ target, profile }: { target: DatabaseTarget; profile?: DatabaseProfile | null }) {
    const selectedProfile = profile || (target?.profiles?.length === 1 ? target.profiles[0] : null);
    if (!selectedProfile) throw new Error("Connector profile is not loaded.");
    const data = connectorConnectionTestResponse(
      await apiPost(`/api/connector-targets/${target.id}/profiles/${selectedProfile.id}/test`, {}),
    );
    return { ok: data.ok, error: data.message || null, data };
  }

  function targetDisplayName({ target }: { target?: DatabasePresentationTarget | null }): string {
    if (!target) return `${label} target`;
    return target.target_name || target.name || `${label} target`;
  }

  function targetSubtitle({ target }: { target: DatabasePresentationTarget }): string {
    return targetEndpoint({ target });
  }

  function targetProfileLabel({ target }: { target?: DatabasePresentationTarget | null }): string {
    return target?.profile_label || "default";
  }

  function deleteDialog({ target }: { target?: DatabaseTarget | null }) {
    return databaseDeleteDialog(label, target);
  }

  async function createTarget(form: DatabaseModelForm<Fields>): Promise<void> {
    await createTargetWithProfile({
      projectID: form.project_id,
      targetPayload: { connector_kind: kind, name: form.name, config: targetConfig(form) },
      profilePayload: profilePayload(form, null, true, targetCredentialPublic(form)),
    });
  }

  async function updateTarget(form: DatabaseModelForm<Fields>, target?: DatabaseTarget | null): Promise<void> {
    const profile =
      target?.profiles?.find((item) => Number(item.id) === Number(form.profile_id)) ||
      (target?.profiles?.length === 1 ? target.profiles[0] : null);
    if (!target || !profile) throw new Error(`${label} connector profile is not loaded.`);
    await updateTargetWithProfile({
      projectID: form.project_id,
      targetID: target.id,
      profileID: profile.id,
      targetPayload: { name: form.name, config: targetConfig(form) },
      profilePayload: profilePayload(form, profile, false, targetCredentialPublic(form)),
    });
  }

  function profilePayload(
    form: Pick<DatabaseCredentialForm, "username" | "password" | "profile_label" | "risk_label">,
    profile: DatabaseProfile | null,
    creating: boolean,
    publicMetadata: Record<string, unknown>,
  ) {
    const payload: {
      kind: string;
      label: string;
      public: Record<string, unknown>;
      risk_label: string;
      secret?: { password?: string };
    } = {
      kind: profile?.kind || "username_password",
      label: form.profile_label,
      public: publicMetadata,
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
    syncEditorForm,
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

function databaseDeleteDialog(label: string, target?: DatabaseTarget | null): ConnectorDeleteDialog {
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

function defaultCredentialMetadata(profile: DatabaseProfile): string[] {
  const items = [];
  if (profile.public?.username) items.push(`username: ${profile.public.username}`);
  if (profile.risk_label) items.push(`risk: ${profile.risk_label}`);
  if (items.length === 0) items.push("No public metadata");
  return items;
}

function defaultCredentialPublic(form: Pick<DatabaseCredentialForm, "username">): Record<string, unknown> {
  return { username: form.username };
}
