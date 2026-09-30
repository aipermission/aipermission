import { SSHConnectorConsoleTemplate, SSHConnectorToolbarActionsTemplate } from "./console";
import { SSHCredentialFormTemplate } from "./credential-form";
import { SSHCredentialRowActionsTemplate } from "./credential-row-actions";
import { SSHConnectorFormTemplate } from "./form";
import { SSHConnectorRowActionsTemplate } from "./list-item";
import * as model from "./model";
import { SSHConnectorOperationsLoader } from "./operations-loader";
import type { ConsoleTemplateContract } from "../console-template-contract";
import { defineConsoleTemplate } from "../_shared/console-template";
import { sshCredentialFamily } from "./credential-family";
export { sshCredentialFamily as credentialFamily } from "./credential-family";
export { sshConnectorFamily as connectorFamily } from "./connector-family";

export default defineConsoleTemplate({
  Console: SSHConnectorConsoleTemplate,
  CredentialForm: SSHCredentialFormTemplate,
  credentialFamily: sshCredentialFamily,
  CredentialRowActions: SSHCredentialRowActionsTemplate,
  Form: SSHConnectorFormTemplate,
  model,
  Operations: SSHConnectorOperationsLoader,
  RowActions: SSHConnectorRowActionsTemplate,
  ToolbarActions: SSHConnectorToolbarActionsTemplate,
} satisfies ConsoleTemplateContract);
export { sshConsoleModel as consoleModel } from "./console-model";
export { sshConsoleRecovery as consoleRecovery } from "./console-recovery";
