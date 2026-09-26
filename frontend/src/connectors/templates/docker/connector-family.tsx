import { defineConnectorFamily } from "../_shared/connector-family-registration";
import { dockerCredentialTargets } from "./credential-family";
import { DockerConnectorFormTemplate } from "./form";
import { DockerConnectorRowActionsTemplate } from "./list-item";
import { DockerConnectorOperationsTemplate } from "./operations";
import * as model from "./model";

type Form = ReturnType<typeof model.emptyForm>;
type Target = ReturnType<typeof dockerCredentialTargets>[number];
type Profile = Target["profiles"][number];
type Operation = { open: boolean; connector_kind: string };

export const dockerConnectorFamily = defineConnectorFamily<Form, Profile, Target, never, never, Operation>({
  kind: "docker",
  decodeTargets: dockerCredentialTargets,
  decodeCredentials: () => [],
  emptyForm: model.emptyForm,
  emptyOperation: () => ({ open: false, connector_kind: "docker" }),
  model,
  tableModel: model,
  deleteDialog: model.deleteDialog,
  renderForm: ({ form, targets, onChange }) => <DockerConnectorFormTemplate form={form} targets={targets} onChange={onChange} />,
  renderRowActions: () => <DockerConnectorRowActionsTemplate />,
  renderOperations: () => <DockerConnectorOperationsTemplate />,
});
