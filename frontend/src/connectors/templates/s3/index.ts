import { S3ConnectorConsoleTemplate } from "./console";
import { S3CredentialFormTemplate } from "./credential-form";
import { S3ConnectorFormTemplate } from "./form";
import { S3ConnectorRowActionsTemplate } from "./list-item";
import { S3ConnectorOperationsTemplate } from "./operations";
import * as model from "./model";
import type { ConsoleTemplateContract } from "../console-template-contract";

export default Object.freeze({
  Console: S3ConnectorConsoleTemplate,
  CredentialForm: S3CredentialFormTemplate,
  Form: S3ConnectorFormTemplate,
  model,
  Operations: S3ConnectorOperationsTemplate,
  RowActions: S3ConnectorRowActionsTemplate,
} satisfies ConsoleTemplateContract);
