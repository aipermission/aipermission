import { defineCredentialFamily } from "../_shared/credential-family-registration";
import { credentialDisplayRow } from "../_shared/credential-display-row";
import { databaseCredentialTargets } from "../_shared/database-credential-targets";
import { optionalConsoleBoolean, optionalConsoleNumber, optionalConsoleText } from "../_shared/console-target-config";
import type { InventoryTarget } from "../../../lib/gateway-contracts/connector-inventory-contract";
import { PostgresCredentialFormTemplate } from "./credential-form";
import * as model from "./model";

type Target = ReturnType<typeof postgresCredentialTargets>[number];
type State = ReturnType<typeof model.emptyCredentialState>;
type Profile = Target["profiles"][number];
type Row = ReturnType<typeof model.credentialRows<Profile, Target>>[number];

export const postgresCredentialFamily = defineCredentialFamily<State, Row, Target, "create" | "update">({
  kind: "postgres",
  label: "Postgres",
  decodeTargets: postgresCredentialTargets,
  emptyState: model.emptyCredentialState,
  model,
  rows: ({ targets }) => model.credentialRows<Profile, Target>({ targets }),
  displayRow: credentialDisplayRow,
  deleteDialog: model.credentialDeleteDialog,
  renderForm: ({ editor, targets }) => (
    <PostgresCredentialFormTemplate
      {...model.credentialFormProps({
        targets,
        formState: editor.formState,
        setFormState: editor.setFormState,
        formMode: editor.drawer.mode,
        state: editor.actionState,
        onSubmit: editor.save,
      })}
    />
  ),
});

export function postgresCredentialTargets(targets: readonly InventoryTarget[]) {
  return databaseCredentialTargets(targets, "postgres", "Postgres").map((target) => ({
    ...target,
    profiles: target.profiles.map((profile) => ({
      ...profile,
      public: {
        ...profile.public,
        managed_by_aipermission: optionalConsoleBoolean(profile.public.managed_by_aipermission, "Postgres", "managed_by_aipermission"),
        managed_admin_profile_id: optionalConsoleNumber(profile.public.managed_admin_profile_id, "Postgres", "managed_admin_profile_id"),
        managed_role_name: optionalConsoleText(profile.public.managed_role_name, "Postgres", "managed_role_name"),
      },
    })),
  }));
}
