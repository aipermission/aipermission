import { DockerConnectorConsoleTemplate } from "./console";
import { DockerCredentialFormTemplate } from "./credential-form";
import { dockerCredentialFamily } from "./credential-family";

export { dockerCredentialFamily as credentialFamily } from "./credential-family";
export { dockerConnectorFamily as connectorFamily } from "./connector-family";
import { DockerConnectorFormTemplate } from "./form";
import { DockerConnectorRowActionsTemplate } from "./list-item";
import { DockerConnectorOperationsTemplate } from "./operations";
import * as model from "./model";
import type { ConsoleTemplateContract } from "../console-template-contract";
import { defineConsoleTemplate } from "../_shared/console-template";

export default defineConsoleTemplate({
  credentialFamily: dockerCredentialFamily,
  Console: DockerConnectorConsoleTemplate,
  CredentialForm: DockerCredentialFormTemplate,
  Form: DockerConnectorFormTemplate,
  model,
  Operations: DockerConnectorOperationsTemplate,
  RowActions: DockerConnectorRowActionsTemplate,
} satisfies ConsoleTemplateContract);
export { dockerConsoleModel as consoleModel } from "./console-model";
export const consoleRecovery = null;
