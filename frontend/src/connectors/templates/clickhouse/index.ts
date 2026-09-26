import { ClickHouseConnectorConsoleTemplate, ClickHouseConnectorToolbarActionsTemplate } from "./console";
import { ClickHouseCredentialFormTemplate } from "./credential-form";
import { ClickHouseConnectorFormTemplate } from "./form";
import { ClickHouseConnectorRowActionsTemplate } from "./list-item";
import * as model from "./model";
import type { ConsoleTemplateContract } from "../console-template-contract";

export default Object.freeze({
  Console: ClickHouseConnectorConsoleTemplate,
  CredentialForm: ClickHouseCredentialFormTemplate,
  Form: ClickHouseConnectorFormTemplate,
  model,
  RowActions: ClickHouseConnectorRowActionsTemplate,
  ToolbarActions: ClickHouseConnectorToolbarActionsTemplate,
} satisfies ConsoleTemplateContract);
