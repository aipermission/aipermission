import { RabbitMQConnectorConsoleTemplate } from "./console";
import { RabbitMQCredentialFormTemplate } from "./credential-form";
import { rabbitCredentialFamily } from "./credential-family";

export { rabbitCredentialFamily as credentialFamily } from "./credential-family";
import { RabbitMQConnectorFormTemplate } from "./form";
import { RabbitMQConnectorRowActionsTemplate } from "./list-item";
import { RabbitMQConnectorOperationsTemplate } from "./operations";
import * as model from "./model";
import type { ConsoleTemplateContract } from "../console-template-contract";

export default Object.freeze({
  credentialFamily: rabbitCredentialFamily,
  Console: RabbitMQConnectorConsoleTemplate,
  CredentialForm: RabbitMQCredentialFormTemplate,
  Form: RabbitMQConnectorFormTemplate,
  model,
  Operations: RabbitMQConnectorOperationsTemplate,
  RowActions: RabbitMQConnectorRowActionsTemplate,
} satisfies ConsoleTemplateContract);
