import { RabbitMQConnectorConsoleTemplate } from "./console";
import { RabbitMQCredentialFormTemplate } from "./credential-form";
import { RabbitMQConnectorFormTemplate } from "./form";
import { RabbitMQConnectorRowActionsTemplate } from "./list-item";
import { RabbitMQConnectorOperationsTemplate } from "./operations";
import * as model from "./model";

export default Object.freeze({
  Console: RabbitMQConnectorConsoleTemplate,
  CredentialForm: RabbitMQCredentialFormTemplate,
  Form: RabbitMQConnectorFormTemplate,
  model,
  Operations: RabbitMQConnectorOperationsTemplate,
  RowActions: RabbitMQConnectorRowActionsTemplate,
});
