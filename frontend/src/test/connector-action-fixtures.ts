import type { ConnectorActionResponse } from "../lib/gateway-contracts/security-contracts";

export function connectorActionRequest(value: unknown): { action_name: string; target_ref: string; input: Record<string, unknown> } {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("Invalid fixture action request.");
  const data = value as Record<string, unknown>;
  if (typeof data.action_name !== "string" || typeof data.target_ref !== "string" ||
      !data.input || typeof data.input !== "object" || Array.isArray(data.input))
    throw new Error("Invalid fixture action request.");
  return { action_name: data.action_name, target_ref: data.target_ref, input: data.input as Record<string, unknown> };
}

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
