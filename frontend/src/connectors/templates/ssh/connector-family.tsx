import { defineConnectorFamily } from "../_shared/connector-family-registration";
import { sshCredentialTargets } from "./credential-family";
import { sshCredentialResourcesResponse } from "./model-helpers";
import { SSHConnectorFormTemplate } from "./form";
import { SSHConnectorRowActionsTemplate } from "./list-item";
import { SSHConnectorOperationsTemplate } from "./operations";
import type { SSHForm } from "./form-types";
import type { SSHCredentialResource } from "./model-types";
import type { SSHOperation } from "./operation-types";
import * as model from "./model";

type Target = ReturnType<typeof sshCredentialTargets>[number];
type Profile = Target["profiles"][number];

export const sshConnectorFamily = defineConnectorFamily<
  SSHForm,
  Profile,
  Target,
  SSHCredentialResource,
  SSHCredentialResource,
  SSHOperation
>({
  kind: "ssh",
  decodeTargets: sshCredentialTargets,
  decodeCredentials: (credentials) =>
    sshCredentialResourcesResponse(credentials.filter((credential) => credential.connector_kind === "ssh")),
  firstCredentialID: (credentials) => credentials[0]?.id || "",
  emptyForm: model.emptyForm,
  emptyOperation: () => ({ open: false, connector_kind: "ssh" }),
  model,
  tableModel: model,
  deleteDialog: model.deleteDialog,
  renderForm: ({ form, credentials, activeCredential, onChange }) => (
    <SSHConnectorFormTemplate form={form} credentials={credentials} activeCredential={activeCredential} onChange={onChange} />
  ),
  renderRowActions: ({ target, profile, onOperation }) => (
    <SSHConnectorRowActionsTemplate target={target} profile={profile} onOperation={onOperation} />
  ),
  renderOperations: ({ value, credentials, onChange, onOperationComplete }) => (
    <SSHConnectorOperationsTemplate value={value} credentials={credentials} onChange={onChange} onOperationComplete={onOperationComplete} />
  ),
});
