import { MailConnectorConsoleTemplate } from "./console";
import { MailCredentialFormTemplate } from "./credential-form";
import { MailConnectorFormTemplate } from "./form";
import { MailConnectorRowActionsTemplate } from "./list-item";
import * as model from "./model";

export default Object.freeze({
  Console: MailConnectorConsoleTemplate,
  CredentialForm: MailCredentialFormTemplate,
  Form: MailConnectorFormTemplate,
  model,
  RowActions: MailConnectorRowActionsTemplate,
});
