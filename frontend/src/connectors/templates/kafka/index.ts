import { KafkaConnectorConsoleTemplate } from "./console";
import { KafkaCredentialFormTemplate } from "./credential-form";
import { kafkaCredentialFamily } from "./credential-family";

export { kafkaCredentialFamily as credentialFamily } from "./credential-family";
export { kafkaConnectorFamily as connectorFamily } from "./connector-family";
import { KafkaConnectorFormTemplate } from "./form";
import { KafkaConnectorRowActionsTemplate } from "./list-item";
import { KafkaConnectorOperationsTemplate } from "./operations";
import * as model from "./model";
import type { ConsoleTemplateContract } from "../console-template-contract";
import { defineConsoleTemplate } from "../_shared/console-template";

export default defineConsoleTemplate({
  credentialFamily: kafkaCredentialFamily,
  Console: KafkaConnectorConsoleTemplate,
  CredentialForm: KafkaCredentialFormTemplate,
  Form: KafkaConnectorFormTemplate,
  model,
  Operations: KafkaConnectorOperationsTemplate,
  RowActions: KafkaConnectorRowActionsTemplate,
} satisfies ConsoleTemplateContract);
export { kafkaConsoleModel as consoleModel } from "./console-model";
export const consoleRecovery = null;
