import { KafkaConnectorConsoleTemplate } from "./console";
import { KafkaCredentialFormTemplate } from "./credential-form";
import { KafkaConnectorFormTemplate } from "./form";
import { KafkaConnectorRowActionsTemplate } from "./list-item";
import { KafkaConnectorOperationsTemplate } from "./operations";
import * as model from "./model";
import type { ConsoleTemplateContract } from "../console-template-contract";

export default Object.freeze({
  Console: KafkaConnectorConsoleTemplate,
  CredentialForm: KafkaCredentialFormTemplate,
  Form: KafkaConnectorFormTemplate,
  model,
  Operations: KafkaConnectorOperationsTemplate,
  RowActions: KafkaConnectorRowActionsTemplate,
} satisfies ConsoleTemplateContract);
