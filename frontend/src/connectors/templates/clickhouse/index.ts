import { ClickHouseConnectorConsoleTemplate, ClickHouseConnectorToolbarActionsTemplate } from "./console";
import { ClickHouseCredentialFormTemplate } from "./credential-form";
import { ClickHouseConnectorFormTemplate } from "./form";
import { ClickHouseConnectorRowActionsTemplate } from "./list-item";
import * as model from "./model";

export default Object.freeze({
  Console: ClickHouseConnectorConsoleTemplate,
  CredentialForm: ClickHouseCredentialFormTemplate,
  Form: ClickHouseConnectorFormTemplate,
  model,
  RowActions: ClickHouseConnectorRowActionsTemplate,
  ToolbarActions: ClickHouseConnectorToolbarActionsTemplate,
});
