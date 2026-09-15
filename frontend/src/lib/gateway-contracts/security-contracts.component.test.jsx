import { describe, expect, it } from "vitest";
import { connectorActionResponse, connectorApprovals, consoleSessions, tokenActionPermissions, vaultApprovals } from "./security-contracts";

describe("typed untrusted gateway contracts", () => {
  const action = (overrides = {}) => ({
    status: "completed",
    request_id: 1,
    target_ref: "test:1:1",
    connector_kind: "test",
    action_name: "list_items",
    retry_policy: { class: "read_only", guidance: "Safe to retry." },
    ...overrides,
  });

  it("requires complete connector action envelopes", () => {
    expect(() => connectorActionResponse(null)).toThrow(/Invalid connector/);
    expect(() => connectorActionResponse({ status: "running" })).toThrow(/Invalid connector/);
    expect(() => connectorActionResponse(action({ request_id: "1" }))).toThrow(/Invalid connector/);
    expect(() => connectorActionResponse(action({ request_id: 0 }))).toThrow(/Invalid connector/);
    expect(() => connectorActionResponse(action({ display_text: 1 }))).toThrow(/Invalid connector/);
    expect(() => connectorActionResponse(action({ error: { message: "failed" } }))).toThrow(/Invalid connector/);
    expect(() => connectorActionResponse(action({ input: [] }))).toThrow(/Invalid connector/);
    expect(() => connectorActionResponse(action({ retry_after_seconds: 1.5 }))).toThrow(/Invalid connector/);
    expect(() => connectorActionResponse(action({ retry_after_seconds: -1 }))).toThrow(/Invalid connector/);
    expect(() => connectorActionResponse(action({ retry_after_seconds: 3601 }))).toThrow(/Invalid connector/);
    expect(() => connectorActionResponse(action({ output_withheld: "true" }))).toThrow(/Invalid connector/);
    expect(() => connectorActionResponse(action({ replayed: 1 }))).toThrow(/Invalid connector/);
    expect(() => connectorActionResponse(action({ status: "future_status" }))).toThrow(/Invalid connector/);
    expect(() => connectorActionResponse(action({ retry_policy: { class: "future", guidance: "" } }))).toThrow(/Invalid connector/);
    expect(() =>
      connectorActionResponse(action({ retry_policy: { class: "read_only", guidance: "Safe to retry.", precondition_fields: [1] } })),
    ).toThrow(/Invalid connector/);
    expect(() => connectorActionResponse(action({ retry_policy: undefined }))).toThrow(/Invalid connector/);
    expect(() => connectorActionResponse(action(), { targetRef: "other:1:1", actionName: "list_items" })).toThrow(/Invalid connector/);
    expect(connectorActionResponse(action({ output: [1, "two", null] }))).toMatchObject({ status: "completed", output: [1, "two", null] });
    expect(
      connectorActionResponse(action({ input: { limit: 20 }, error: "", retry_after_seconds: 3, output_withheld: false, replayed: true })),
    ).toMatchObject({ retry_after_seconds: 3, replayed: true });
  });

  it("fails closed on malformed token permission and approval entries", () => {
    expect(() => tokenActionPermissions({ items: null })).toThrow(/Invalid token/);
    expect(() => tokenActionPermissions({ items: [{ target_id: 1, profile_id: 2, action_name: "exec" }] })).toThrow(/Invalid token/);
    expect(() => connectorApprovals([{ id: 1, status: "approval_pending" }])).toThrow(/Invalid connector/);
    expect(() => vaultApprovals([{ id: "1", status: "approval_pending" }])).toThrow(/Invalid Vault/);
    expect(vaultApprovals([vaultApproval()])).toHaveLength(1);
    expect(() =>
      tokenActionPermissions({ items: [{ target_id: 1, profile_id: 2, action_name: "exec", execution_rule: "future_rule" }] }),
    ).toThrow(/Invalid token/);
    expect(
      tokenActionPermissions({ items: [{ target_id: 1, profile_id: 2, action_name: "exec", execution_rule: "approval_required" }] }),
    ).toHaveLength(1);
  });

  it("rejects malformed console sessions without activating a socket", () => {
    expect(() => consoleSessions([{ id: 0 }])).toThrow(/Invalid console session/);
    expect(consoleSessions([{ id: 1, status: "active" }])).toHaveLength(1);
  });
});

function vaultApproval(overrides = {}) {
  return {
    id: 1,
    status: "approval_pending",
    token_id: 7,
    token_name: "codex",
    project_id: 3,
    project_name: "My Project",
    project_slug: "my-project",
    action_name: "generate_item",
    source: "mcp",
    input: {},
    reason: "coverage",
    approval_context_hash: "context-hash",
    idempotency_key: "fixture-key",
    created_at: "2026-09-16T00:00:00Z",
    expires_at: "2026-09-16T00:15:00Z",
    updated_at: "2026-09-16T00:00:00Z",
    ...overrides,
  };
}
