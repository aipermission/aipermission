import { defineConnectorFamily } from "../_shared/connector-family-registration";
import { kafkaCredentialTargets } from "./credential-family";
import { KafkaConnectorFormTemplate } from "./form";
import { KafkaConnectorRowActionsTemplate } from "./list-item";
import { KafkaConnectorOperationsTemplate } from "./operations";
import * as model from "./model";

type Form = ReturnType<typeof model.emptyForm>;
type Target = ReturnType<typeof kafkaCredentialTargets>[number];
type Profile = Target["profiles"][number];
type Operation = { open: boolean; connector_kind: string };

export const kafkaConnectorFamily = defineConnectorFamily<Form, Profile, Target, never, never, Operation>({
  kind: "kafka",
  decodeTargets: kafkaCredentialTargets,
  decodeCredentials: () => [],
  emptyForm: model.emptyForm,
  emptyOperation: () => ({ open: false, connector_kind: "kafka" }),
  model,
  tableModel: model,
  deleteDialog: model.deleteDialog,
  renderForm: ({ form, mode, targets, onChange }) => (
    <KafkaConnectorFormTemplate form={form} mode={mode} targets={targets} onChange={onChange} />
  ),
  renderRowActions: () => <KafkaConnectorRowActionsTemplate />,
  renderOperations: () => <KafkaConnectorOperationsTemplate />,
});
