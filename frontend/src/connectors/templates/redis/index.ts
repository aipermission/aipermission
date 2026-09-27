import { RedisConnectorConsoleTemplate } from "./console";
import { RedisCredentialFormTemplate } from "./credential-form";
import { RedisConnectorFormTemplate } from "./form";
import { RedisConnectorRowActionsTemplate } from "./list-item";
import { RedisConnectorOperationsTemplate } from "./operations";
import * as model from "./model";
import type { ConsoleTemplateContract } from "../console-template-contract";
import { defineConsoleTemplate } from "../_shared/console-template";
import { redisCredentialFamily } from "./credential-family";
export { redisCredentialFamily as credentialFamily } from "./credential-family";
export { redisConnectorFamily as connectorFamily } from "./connector-family";
export { redisConsoleModel as consoleModel } from "./console-model";
export const consoleRecovery = null;

export default defineConsoleTemplate({
  Console: RedisConnectorConsoleTemplate,
  CredentialForm: RedisCredentialFormTemplate,
  credentialFamily: redisCredentialFamily,
  Form: RedisConnectorFormTemplate,
  model,
  Operations: RedisConnectorOperationsTemplate,
  RowActions: RedisConnectorRowActionsTemplate,
} satisfies ConsoleTemplateContract);
