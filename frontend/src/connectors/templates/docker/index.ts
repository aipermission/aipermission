import { DockerConnectorConsoleTemplate } from "./console";
import { DockerCredentialFormTemplate } from "./credential-form";
import { DockerConnectorFormTemplate } from "./form";
import { DockerConnectorRowActionsTemplate } from "./list-item";
import { DockerConnectorOperationsTemplate } from "./operations";
import * as model from "./model";

export default Object.freeze({
  Console: DockerConnectorConsoleTemplate,
  CredentialForm: DockerCredentialFormTemplate,
  Form: DockerConnectorFormTemplate,
  model,
  Operations: DockerConnectorOperationsTemplate,
  RowActions: DockerConnectorRowActionsTemplate,
});
