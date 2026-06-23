import { KubernetesConnectorConsoleTemplate } from "./console";
import { KubernetesCredentialFormTemplate } from "./credential-form";
import { KubernetesConnectorFormTemplate } from "./form";
import { KubernetesConnectorRowActionsTemplate } from "./list-item";
import { KubernetesConnectorOperationsTemplate } from "./operations";
import * as model from "./model";

export default Object.freeze({
  Console: KubernetesConnectorConsoleTemplate,
  CredentialForm: KubernetesCredentialFormTemplate,
  Form: KubernetesConnectorFormTemplate,
  model,
  Operations: KubernetesConnectorOperationsTemplate,
  RowActions: KubernetesConnectorRowActionsTemplate,
});
