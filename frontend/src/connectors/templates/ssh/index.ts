import { SSHConnectorConsoleTemplate, SSHConnectorToolbarActionsTemplate } from "./console";
import { SSHCredentialFormTemplate } from "./credential-form";
import { SSHCredentialRowActionsTemplate } from "./credential-row-actions";
import { SSHConnectorFormTemplate } from "./form";
import { SSHConnectorRowActionsTemplate } from "./list-item";
import * as model from "./model";
import { SSHConnectorOperationsTemplate } from "./operations";

export default Object.freeze({
  Console: SSHConnectorConsoleTemplate,
  CredentialForm: SSHCredentialFormTemplate,
  CredentialRowActions: SSHCredentialRowActionsTemplate,
  Form: SSHConnectorFormTemplate,
  model,
  Operations: SSHConnectorOperationsTemplate,
  RowActions: SSHConnectorRowActionsTemplate,
  ToolbarActions: SSHConnectorToolbarActionsTemplate,
});
