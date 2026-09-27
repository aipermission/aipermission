import { ClickHouseConnectorConsoleTemplate, ClickHouseConnectorToolbarActionsTemplate } from "./console";
import { ClickHouseCredentialFormTemplate } from "./credential-form";
import { ClickHouseConnectorFormTemplate } from "./form";
import { ClickHouseConnectorRowActionsTemplate } from "./list-item";
import * as model from "./model";
import type { ConsoleTemplateContract } from "../console-template-contract";
import { defineConsoleTemplate } from "../_shared/console-template";
import { clickHouseCredentialFamily } from "./credential-family";

export { clickHouseCredentialFamily as credentialFamily } from "./credential-family";
export { clickHouseConnectorFamily as connectorFamily } from "./connector-family";

export default defineConsoleTemplate({
  credentialFamily: clickHouseCredentialFamily,
  Console: ClickHouseConnectorConsoleTemplate,
  CredentialForm: ClickHouseCredentialFormTemplate,
  Form: ClickHouseConnectorFormTemplate,
  model,
  RowActions: ClickHouseConnectorRowActionsTemplate,
  ToolbarActions: ClickHouseConnectorToolbarActionsTemplate,
} satisfies ConsoleTemplateContract);
export { clickhouseConsoleModel as consoleModel } from "./console-model";
export const consoleRecovery = null;
