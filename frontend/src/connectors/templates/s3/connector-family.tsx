import { defineConnectorFamily } from "../_shared/connector-family-registration";
import { s3CredentialTargets } from "./credential-family";
import { S3ConnectorFormTemplate } from "./form";
import { S3ConnectorRowActionsTemplate } from "./list-item";
import { S3ConnectorOperationsTemplate } from "./operations";
import * as model from "./model";

type Form = ReturnType<typeof model.emptyForm>;
type Target = ReturnType<typeof s3CredentialTargets>[number];
type Profile = Target["profiles"][number];
type Operation = { open: boolean; connector_kind: string };

export const s3ConnectorFamily = defineConnectorFamily<Form, Profile, Target, never, never, Operation>({
  kind: "s3",
  decodeTargets: s3CredentialTargets,
  decodeCredentials: () => [],
  emptyForm: model.emptyForm,
  emptyOperation: () => ({ open: false, connector_kind: "s3" }),
  model,
  tableModel: model,
  deleteDialog: model.deleteDialog,
  renderForm: ({ form, mode, targets, onChange }) => (
    <S3ConnectorFormTemplate form={form} mode={mode} targets={targets} onChange={onChange} />
  ),
  renderRowActions: () => <S3ConnectorRowActionsTemplate />,
  renderOperations: () => <S3ConnectorOperationsTemplate />,
});
