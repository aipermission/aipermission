import { defineConnectorFamily } from "../_shared/connector-family-registration";
import { postgresCredentialTargets } from "./credential-family";
import { PostgresConnectorFormTemplate } from "./form";
import { PostgresConnectorRowActionsTemplate } from "./list-item";
import { PostgresConnectorOperationsTemplate } from "./operations";
import type { PostgresOperation } from "./operation-types";
import * as model from "./model";

type Form = ReturnType<typeof model.emptyForm>;
type Target = ReturnType<typeof postgresCredentialTargets>[number];
type Profile = Target["profiles"][number];

export const postgresConnectorFamily = defineConnectorFamily<Form, Profile, Target, never, never, PostgresOperation>({
  kind: "postgres",
  decodeTargets: postgresCredentialTargets,
  decodeCredentials: () => [],
  emptyForm: model.emptyForm,
  emptyOperation: () => ({ open: false }),
  model: { ...model, syncForm: model.syncEditorForm },
  tableModel: model,
  deleteDialog: model.deleteDialog,
  renderForm: ({ form, mode, targets, onChange }) => (
    <PostgresConnectorFormTemplate form={form} mode={mode} targets={targets} onChange={onChange} />
  ),
  renderRowActions: ({ target, profile, onOperation }) => (
    <PostgresConnectorRowActionsTemplate target={target} profile={profile} onOperation={onOperation} />
  ),
  renderOperations: ({ value, onChange, onOperationComplete }) => (
    <PostgresConnectorOperationsTemplate value={value} onChange={onChange} onOperationComplete={onOperationComplete} />
  ),
});
