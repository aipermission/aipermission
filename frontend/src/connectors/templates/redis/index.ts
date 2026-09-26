import { RedisConnectorConsoleTemplate } from "./console";
import { RedisCredentialFormTemplate } from "./credential-form";
import { RedisConnectorFormTemplate } from "./form";
import { RedisConnectorRowActionsTemplate } from "./list-item";
import { RedisConnectorOperationsTemplate } from "./operations";
import * as model from "./model";
import type { ConsoleTemplateContract } from "../console-template-contract";
import { redisCredentialFamily } from "./credential-family";
export { redisCredentialFamily as credentialFamily } from "./credential-family";

export default Object.freeze({
  Console: RedisConnectorConsoleTemplate,
  CredentialForm: RedisCredentialFormTemplate,
  credentialFamily: redisCredentialFamily,
  Form: RedisConnectorFormTemplate,
  model,
  Operations: RedisConnectorOperationsTemplate,
  RowActions: RedisConnectorRowActionsTemplate,
} satisfies ConsoleTemplateContract);
