import { defineConnectorFamily } from "../_shared/connector-family-registration";
import { mailCredentialTargets } from "./credential-family";
import { MailConnectorFormTemplate } from "./form";
import { MailConnectorRowActionsTemplate } from "./list-item";
import * as model from "./model";

type Form = ReturnType<typeof model.emptyForm>;
type Target = ReturnType<typeof mailCredentialTargets>[number];
type Profile = Target["profiles"][number];
type Operation = { open: boolean; connector_kind: string };

export const mailConnectorFamily = defineConnectorFamily<Form, Profile, Target, never, never, Operation>({
  kind: "mail",
  decodeTargets: mailCredentialTargets,
  decodeCredentials: () => [],
  emptyForm: model.emptyForm,
  emptyOperation: () => ({ open: false, connector_kind: "mail" }),
  model,
  tableModel: model,
  deleteDialog: model.deleteDialog,
  renderForm: ({ form, mode, targets, onChange }) => (
    <MailConnectorFormTemplate form={form} mode={mode} targets={targets} onChange={onChange} />
  ),
  renderRowActions: () => <MailConnectorRowActionsTemplate />,
  renderOperations: () => null,
});
