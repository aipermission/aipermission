import { defineConsoleTemplate } from "../_shared/console-template";
import type { ConsoleTemplateContract } from "../console-template-contract";
import { SQLServerConnectorConsoleTemplate, SQLServerConnectorToolbarActionsTemplate } from "./console";
import { SQLServerCredentialFormTemplate } from "./credential-form";
import { SQLServerConnectorFormTemplate } from "./form";
import { SQLServerConnectorRowActionsTemplate } from "./list-item";
import { sqlServerCredentialFamily } from "./credential-family";
import * as model from "./model";

export { sqlServerCredentialFamily as credentialFamily } from "./credential-family";
export { sqlServerConnectorFamily as connectorFamily } from "./connector-family";
export { sqlServerConsoleModel as consoleModel } from "./console-model";
export const consoleRecovery = null;
export default defineConsoleTemplate({
  credentialFamily: sqlServerCredentialFamily,
  Console: SQLServerConnectorConsoleTemplate,
  ToolbarActions: SQLServerConnectorToolbarActionsTemplate,
  CredentialForm: SQLServerCredentialFormTemplate,
  Form: SQLServerConnectorFormTemplate,
  RowActions: SQLServerConnectorRowActionsTemplate,
  model,
} satisfies ConsoleTemplateContract);
