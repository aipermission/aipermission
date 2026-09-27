import { MailConnectorConsoleTemplate } from "./console";
import { MailCredentialFormTemplate } from "./credential-form";
import { mailCredentialFamily } from "./credential-family";

export { mailCredentialFamily as credentialFamily } from "./credential-family";
export { mailConnectorFamily as connectorFamily } from "./connector-family";
import { MailConnectorFormTemplate } from "./form";
import { MailConnectorRowActionsTemplate } from "./list-item";
import * as model from "./model";
import type { ConsoleTemplateContract } from "../console-template-contract";

export default Object.freeze({
  credentialFamily: mailCredentialFamily,
  Console: MailConnectorConsoleTemplate,
  CredentialForm: MailCredentialFormTemplate,
  Form: MailConnectorFormTemplate,
  model,
  RowActions: MailConnectorRowActionsTemplate,
} satisfies ConsoleTemplateContract);
export { mailConsoleModel as consoleModel } from "./console-model";
export const consoleRecovery = null;
