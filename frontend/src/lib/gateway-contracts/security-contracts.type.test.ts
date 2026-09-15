import {
  connectorActionResponse,
  type ConnectorActionResponse,
  type ConnectorActionStatus,
  type ExecutionRule,
} from "./security-contracts";

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
void status;
void rule;

// @ts-expect-error Unknown gateway statuses must not silently enter the typed contract.
const unknownStatus: ConnectorActionStatus = "future_status";
// @ts-expect-error Permission rules are a closed authorization contract.
const unknownRule: ExecutionRule = "future_rule";
void unknownStatus;
void unknownRule;
