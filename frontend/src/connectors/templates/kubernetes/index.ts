import { KubernetesConnectorConsoleTemplate } from "./console";
import { KubernetesCredentialFormTemplate } from "./credential-form";
import { kubernetesCredentialFamily } from "./credential-family";

export { kubernetesCredentialFamily as credentialFamily } from "./credential-family";
import { KubernetesConnectorFormTemplate } from "./form";
import { KubernetesConnectorRowActionsTemplate } from "./list-item";
import { KubernetesConnectorOperationsTemplate } from "./operations";
import * as model from "./model";
import type { ConsoleTemplateContract } from "../console-template-contract";

export default Object.freeze({
  credentialFamily: kubernetesCredentialFamily,
  Console: KubernetesConnectorConsoleTemplate,
  CredentialForm: KubernetesCredentialFormTemplate,
  Form: KubernetesConnectorFormTemplate,
  model,
  Operations: KubernetesConnectorOperationsTemplate,
  RowActions: KubernetesConnectorRowActionsTemplate,
} satisfies ConsoleTemplateContract);
