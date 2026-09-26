import { defineConnectorFamily } from "../_shared/connector-family-registration";
import { clickHouseCredentialTargets } from "./credential-family";
import { ClickHouseConnectorFormTemplate } from "./form";
import { ClickHouseConnectorRowActionsTemplate } from "./list-item";
import * as model from "./model";

type Form = ReturnType<typeof model.emptyForm>;
type Target = ReturnType<typeof clickHouseCredentialTargets>[number];
type Profile = Target["profiles"][number];
type Operation = { open: boolean; connector_kind: string };

export const clickHouseConnectorFamily = defineConnectorFamily<Form, Profile, Target, never, never, Operation>({
  kind: "clickhouse",
  decodeTargets: clickHouseCredentialTargets,
  decodeCredentials: () => [],
  emptyForm: model.emptyForm,
  emptyOperation: () => ({ open: false, connector_kind: "clickhouse" }),
  model: { ...model, syncForm: model.syncEditorForm },
  tableModel: model,
  deleteDialog: model.deleteDialog,
  renderForm: ({ form, mode, targets, onChange }) => (
    <ClickHouseConnectorFormTemplate form={form} mode={mode} targets={targets} onChange={onChange} />
  ),
  renderRowActions: () => <ClickHouseConnectorRowActionsTemplate />,
  renderOperations: () => null,
});
