import { PostgresConnectorConsoleTemplate, PostgresConnectorToolbarActionsTemplate } from "./console";
import { PostgresCredentialFormTemplate } from "./credential-form";
import { PostgresConnectorFormTemplate } from "./form";
import { PostgresConnectorRowActionsTemplate } from "./list-item";
import * as model from "./model";

export default Object.freeze({
  Console: PostgresConnectorConsoleTemplate,
  CredentialForm: PostgresCredentialFormTemplate,
  Form: PostgresConnectorFormTemplate,
  model,
  RowActions: PostgresConnectorRowActionsTemplate,
  ToolbarActions: PostgresConnectorToolbarActionsTemplate,
});
