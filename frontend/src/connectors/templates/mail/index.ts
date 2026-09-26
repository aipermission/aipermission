import { MailConnectorConsoleTemplate } from "./console";
import { MailCredentialFormTemplate } from "./credential-form";
import { MailConnectorFormTemplate } from "./form";
import { MailConnectorRowActionsTemplate } from "./list-item";
import * as model from "./model";
import type { ConsoleTemplateContract } from "../console-template-contract";

export default Object.freeze({
  Console: MailConnectorConsoleTemplate,
  CredentialForm: MailCredentialFormTemplate,
  Form: MailConnectorFormTemplate,
  model,
  RowActions: MailConnectorRowActionsTemplate,
} satisfies ConsoleTemplateContract);
