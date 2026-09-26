import type { ConnectorActionResponse } from "../lib/gateway-contracts/security-contracts";

export function connectorActionFixture(overrides: Partial<ConnectorActionResponse> = {}): ConnectorActionResponse {
  return {
    request_id: 1,
    target_ref: "example:1:1",
    connector_kind: "example",
    action_name: "example_action",
    status: "completed",
    retry_policy: { class: "read_only", guidance: "Review the current result before retrying." },
    ...overrides,
  };
}
