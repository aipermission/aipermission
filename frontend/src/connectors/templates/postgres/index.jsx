import { PostgresConnectorConsoleTemplate, PostgresConnectorToolbarActionsTemplate } from "./console";
import { PostgresCredentialFormTemplate } from "./credential-form";
import { PostgresConnectorFormTemplate } from "./form";
import { PostgresConnectorRowActionsTemplate } from "./list-item";
import { PostgresConnectorOperationsTemplate } from "./operations";
import * as model from "./model";

export default Object.freeze({
  Console: PostgresConnectorConsoleTemplate,
  CredentialForm: PostgresCredentialFormTemplate,
  Form: PostgresConnectorFormTemplate,
  model,
  Operations: PostgresConnectorOperationsTemplate,
  RowActions: PostgresConnectorRowActionsTemplate,
  ToolbarActions: PostgresConnectorToolbarActionsTemplate,
});
