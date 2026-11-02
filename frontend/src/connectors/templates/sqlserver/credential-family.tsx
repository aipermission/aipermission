import { defineCredentialFamily } from "../_shared/credential-family-registration";
import { credentialDisplayRow } from "../_shared/credential-display-row";
import { databaseCredentialTargets } from "../_shared/database-credential-targets";
import { optionalConsoleText } from "../_shared/console-target-config";
import type { InventoryTarget } from "../../../lib/gateway-contracts/connector-inventory-contract";
import { SQLServerCredentialFormTemplate } from "./credential-form";
import * as model from "./model";

type Target = ReturnType<typeof sqlServerTargets>[number];
type State = ReturnType<typeof model.emptyCredentialState>;
type Profile = Target["profiles"][number];
type Row = ReturnType<typeof model.credentialRows<Profile, Target>>[number];

export function sqlServerTargets(targets: readonly InventoryTarget[]) {
  const configs = new Map(targets.map((target) => [target.id, target.config || {}]));
  return databaseCredentialTargets(targets, "sqlserver", "SQL Server").map((target) => ({
    ...target,
    config: {
      ...target.config,
      tls_mode: optionalConsoleText(configs.get(target.id)?.tls_mode, "SQL Server", "tls_mode"),
      tls_server_name: optionalConsoleText(configs.get(target.id)?.tls_server_name, "SQL Server", "tls_server_name"),
      ca_certificate: optionalConsoleText(configs.get(target.id)?.ca_certificate, "SQL Server", "ca_certificate"),
    },
  }));
}

export const sqlServerCredentialFamily = defineCredentialFamily<State, Row, Target, "create" | "update">({
  kind: "sqlserver",
  label: "SQL Server",
  decodeTargets: sqlServerTargets,
  emptyState: model.emptyCredentialState,
  model,
  rows: ({ targets }) => model.credentialRows<Profile, Target>({ targets }),
  displayRow: credentialDisplayRow,
  renderForm: ({ editor, targets }) => (
    <SQLServerCredentialFormTemplate
      passwordRequired={editor.drawer.mode === "edit" && editor.formState.form.username !== editor.drawer.row?.profile.public.username}
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
