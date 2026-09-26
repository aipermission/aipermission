import { defineConnectorFamily } from "../_shared/connector-family-registration";
import { rabbitCredentialTargets } from "./credential-family";
import { RabbitMQConnectorFormTemplate } from "./form";
import { RabbitMQConnectorRowActionsTemplate } from "./list-item";
import { RabbitMQConnectorOperationsTemplate } from "./operations";
import * as model from "./model";

type Form = ReturnType<typeof model.emptyForm>;
type Target = ReturnType<typeof rabbitCredentialTargets>[number];
type Profile = Target["profiles"][number];
type Operation = { open: boolean; connector_kind: string };

export const rabbitConnectorFamily = defineConnectorFamily<Form, Profile, Target, never, never, Operation>({
  kind: "rabbitmq",
  decodeTargets: rabbitCredentialTargets,
  decodeCredentials: () => [],
  emptyForm: model.emptyForm,
  emptyOperation: () => ({ open: false, connector_kind: "rabbitmq" }),
  model,
  tableModel: model,
  deleteDialog: model.deleteDialog,
  renderForm: ({ form, mode, targets, onChange }) => (
    <RabbitMQConnectorFormTemplate form={form} mode={mode} targets={targets} onChange={onChange} />
  ),
  renderRowActions: () => <RabbitMQConnectorRowActionsTemplate />,
  renderOperations: () => <RabbitMQConnectorOperationsTemplate />,
});
