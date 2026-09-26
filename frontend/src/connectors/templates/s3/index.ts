import { S3ConnectorConsoleTemplate } from "./console";
import { S3CredentialFormTemplate } from "./credential-form";
import { s3CredentialFamily } from "./credential-family";

export { s3CredentialFamily as credentialFamily } from "./credential-family";
export { s3ConnectorFamily as connectorFamily } from "./connector-family";
import { S3ConnectorFormTemplate } from "./form";
import { S3ConnectorRowActionsTemplate } from "./list-item";
import { S3ConnectorOperationsTemplate } from "./operations";
import * as model from "./model";
import type { ConsoleTemplateContract } from "../console-template-contract";

export default Object.freeze({
  credentialFamily: s3CredentialFamily,
  Console: S3ConnectorConsoleTemplate,
  CredentialForm: S3CredentialFormTemplate,
  Form: S3ConnectorFormTemplate,
  model,
  Operations: S3ConnectorOperationsTemplate,
  RowActions: S3ConnectorRowActionsTemplate,
} satisfies ConsoleTemplateContract);
