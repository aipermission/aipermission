import { RabbitMQConnectorConsoleTemplate } from "./console";
import { RabbitMQCredentialFormTemplate } from "./credential-form";
import { RabbitMQConnectorFormTemplate } from "./form";
import { RabbitMQConnectorRowActionsTemplate } from "./list-item";
import { RabbitMQConnectorOperationsTemplate } from "./operations";
import * as model from "./model";
import type { ConsoleTemplateContract } from "../console-template-contract";

export default Object.freeze({
  Console: RabbitMQConnectorConsoleTemplate,
  CredentialForm: RabbitMQCredentialFormTemplate,
  Form: RabbitMQConnectorFormTemplate,
  model,
  Operations: RabbitMQConnectorOperationsTemplate,
  RowActions: RabbitMQConnectorRowActionsTemplate,
} satisfies ConsoleTemplateContract);
