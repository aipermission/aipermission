import { RedisConnectorConsoleTemplate } from "./console";
import { RedisCredentialFormTemplate } from "./credential-form";
import { RedisConnectorFormTemplate } from "./form";
import { RedisConnectorRowActionsTemplate } from "./list-item";
import { RedisConnectorOperationsTemplate } from "./operations";
import * as model from "./model";
import type { ConsoleTemplateContract } from "../console-template-contract";

export default Object.freeze({
  Console: RedisConnectorConsoleTemplate,
  CredentialForm: RedisCredentialFormTemplate,
  Form: RedisConnectorFormTemplate,
  model,
  Operations: RedisConnectorOperationsTemplate,
  RowActions: RedisConnectorRowActionsTemplate,
} satisfies ConsoleTemplateContract);
