import { describe, expect, it } from "vitest";
import { connectorActionResponse, consoleSessions, pendingApprovals, tokenActionPermissions } from "./security-contracts";

describe("typed untrusted gateway contracts", () => {
  it("requires a request id for pending actions and bounds the output shape", () => {
    expect(() => connectorActionResponse({ status: "running" })).toThrow(/request_id/);
    expect(() => connectorActionResponse({ status: "completed", output: [] })).toThrow(/Invalid connector/);
    expect(connectorActionResponse({ status: "completed", output: { count: 1 } })).toMatchObject({ status: "completed" });
  });

  it("fails closed on malformed token permission and approval entries", () => {
    expect(() => tokenActionPermissions({ items: [{ target_id: 1, profile_id: 2, action_name: "exec" }] })).toThrow(/Invalid token/);
    expect(() => pendingApprovals([{ id: "1", status: "approval_pending" }], "Vault approval")).toThrow(/Invalid Vault/);
    expect(
      tokenActionPermissions({ items: [{ target_id: 1, profile_id: 2, action_name: "exec", execution_rule: "approval_required" }] }),
    ).toHaveLength(1);
  });

  it("rejects malformed console sessions without activating a socket", () => {
    expect(() => consoleSessions([{ id: 0 }])).toThrow(/Invalid console session/);
    expect(consoleSessions([{ id: 1, status: "active" }])).toHaveLength(1);
  });
});
