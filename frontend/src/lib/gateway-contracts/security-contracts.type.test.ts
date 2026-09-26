import {
  connectorActionResponse,
  type ConnectorApproval,
  type ConnectorActionResponse,
  type ConnectorActionStatus,
  type ConnectorRetryClass,
  type ConnectorRetryPolicy,
  type ExecutionRule,
  type VaultApproval,
  type VaultSessionItem,
} from "./security-contracts";
import { assertConnectorActionResponse, isConnectorActionStatus, isConnectorRetryPolicy } from "./connector-action-contract";

type Equal<Left, Right> = (<Value>() => Value extends Left ? 1 : 2) extends <Value>() => Value extends Right ? 1 : 2 ? true : false;
const actionStatusMatchesSchema: Equal<ConnectorActionStatus, ConnectorActionResponse["status"]> = true;
const approvalStatusMatchesSchema: Equal<ConnectorActionStatus, ConnectorApproval["status"]> = true;
const retryClassMatchesSchema: Equal<ConnectorRetryClass, ConnectorRetryPolicy["class"]> = true;
void actionStatusMatchesSchema;
void approvalStatusMatchesSchema;
void retryClassMatchesSchema;
const actionValidatorMatchesSchema: Equal<ReturnType<typeof assertConnectorActionResponse>, ConnectorActionResponse> = true;
void actionValidatorMatchesSchema;

const untrustedStatus: unknown = "completed";
if (isConnectorActionStatus(untrustedStatus)) {
  const validatedStatus: ConnectorActionStatus = untrustedStatus;
  void validatedStatus;
}
const untrustedRetryPolicy: unknown = { class: "read_only", guidance: "Safe to retry." };
if (isConnectorRetryPolicy(untrustedRetryPolicy)) {
  const validatedPolicy: ConnectorRetryPolicy = untrustedRetryPolicy;
  void validatedPolicy;
}

const response: ConnectorActionResponse = connectorActionResponse({
  status: "completed",
  request_id: 1,
  target_ref: "test:1:1",
  connector_kind: "test",
  action_name: "list_items",
  retry_policy: { class: "read_only", guidance: "Safe to retry." },
  output: null,
});
if (response.output !== undefined && response.output !== null) Object.keys(response.output);

const status: ConnectorActionStatus = response.status;
const rule: ExecutionRule = "approval_required";
const retryPolicy: ConnectorRetryPolicy = { class: "read_only", guidance: "Safe to retry." };
const vaultItem: VaultSessionItem = {
  item_id: 1,
  name: "API_TOKEN",
  source_project_id: 2,
  value_version: 3,
  metadata_revision: 4,
  replace_existing: false,
};
void status;
void rule;
void retryPolicy;
void vaultItem;

// @ts-expect-error Unknown gateway statuses must not silently enter the typed contract.
const unknownStatus: ConnectorActionStatus = "future_status";
// @ts-expect-error Permission rules are a closed authorization contract.
const unknownRule: ExecutionRule = "future_rule";
// @ts-expect-error Vault approval status is a closed backend contract.
const unknownVaultStatus: VaultApproval["status"] = "future_status";
// @ts-expect-error Retry classes come from the generated gateway schema.
const unknownRetryClass: ConnectorRetryPolicy = { class: "future_class", guidance: "Retry." };
// @ts-expect-error Approval identity is required by the generated gateway schema.
const incompleteApproval: ConnectorApproval = { id: 1 };
// @ts-expect-error Vault session item revisions must be numeric.
const invalidVaultItem: VaultSessionItem = { ...vaultItem, metadata_revision: "4" };
void unknownStatus;
void unknownRule;
void unknownVaultStatus;
void unknownRetryClass;
void incompleteApproval;
void invalidVaultItem;
