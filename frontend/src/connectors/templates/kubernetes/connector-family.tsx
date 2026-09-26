import { defineConnectorFamily } from "../_shared/connector-family-registration";
import { kubernetesCredentialTargets } from "./credential-family";
import { KubernetesConnectorFormTemplate } from "./form";
import { KubernetesConnectorRowActionsTemplate } from "./list-item";
import { KubernetesConnectorOperationsTemplate } from "./operations";
import * as model from "./model";

type Form = ReturnType<typeof model.emptyForm>;
type Target = ReturnType<typeof kubernetesCredentialTargets>[number];
type Profile = Target["profiles"][number];
type Operation = { open: boolean; connector_kind: string };

export const kubernetesConnectorFamily = defineConnectorFamily<Form, Profile, Target, never, never, Operation>({
  kind: "kubernetes",
  decodeTargets: kubernetesCredentialTargets,
  decodeCredentials: () => [],
  emptyForm: model.emptyForm,
  emptyOperation: () => ({ open: false, connector_kind: "kubernetes" }),
  model,
  tableModel: model,
  deleteDialog: model.deleteDialog,
  renderForm: ({ form, targets, onChange }) => <KubernetesConnectorFormTemplate form={form} targets={targets} onChange={onChange} />,
  renderRowActions: () => <KubernetesConnectorRowActionsTemplate />,
  renderOperations: () => <KubernetesConnectorOperationsTemplate />,
});
