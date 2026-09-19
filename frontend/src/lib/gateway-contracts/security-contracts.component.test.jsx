import { describe, expect, it } from "vitest";
import {
  connectorActionResponse,
  connectorApproval as parseConnectorApproval,
  connectorApprovals,
  consoleSessions,
  tokenActionPermissions,
  vaultApproval as parseVaultApproval,
  vaultApprovals,
} from "./security-contracts";

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
    expect(() => connectorApprovals([connectorApproval({ approval_context_hash: "" })])).toThrow(/Invalid connector/);
    expect(connectorApprovals([connectorApproval({ status: "completed", approval_context_hash: undefined })])).toHaveLength(1);
    expect(() => vaultApprovals([vaultApproval({ approval_context_hash: "" })])).toThrow(/Invalid Vault/);
    expect(vaultApprovals([vaultApproval({ status: "completed", approval_context_hash: "" })])).toHaveLength(1);
    expect(() => vaultApprovals([vaultApproval({ approval_context: { items: {} } })])).toThrow(/Invalid Vault/);
    expect(() =>
      parseVaultApproval(vaultApproval({ status: "completed", approval_context_hash: "" }), { id: 2, statuses: ["completed"] }),
    ).toThrow(/Invalid Vault/);
    expect(() =>
      tokenActionPermissions({ items: [{ target_id: 1, profile_id: 2, action_name: "exec", execution_rule: "future_rule" }] }),
    ).toThrow(/Invalid token/);
    expect(
      tokenActionPermissions({ items: [{ target_id: 1, profile_id: 2, action_name: "exec", execution_rule: "approval_required" }] }),
    ).toHaveLength(1);
  });

  it("binds connector decision envelopes to the displayed approval", () => {
    const expected = { id: 1, targetRef: "fixture:2:3", actionName: "read", statuses: ["completed", "running"] };
    expect(parseConnectorApproval(connectorApproval({ status: "completed", approval_context_hash: "" }), expected)).toMatchObject({
      id: 1,
      status: "completed",
    });
    for (const item of [
      connectorApproval({ id: 2 }),
      connectorApproval({ target_ref: "fixture:9:9" }),
      connectorApproval({ action_name: "write" }),
      connectorApproval({ status: "declined", approval_context_hash: "" }),
    ]) {
      expect(() => parseConnectorApproval(item, expected)).toThrow(/Invalid connector approval/);
    }
    expect(() => parseConnectorApproval(connectorApproval({ status: "completed", approval_context_hash: 7 }))).toThrow(
      /Invalid connector approval/,
    );
  });

  it("rejects malformed optional connector approval display fields", () => {
    for (const overrides of [
      { token_id: 0 },
      { token_name: [] },
      { title: {} },
      { summary: 7 },
      { preview: [] },
      { input: "command" },
      { reason: {} },
      { display_text: [] },
      { error: { message: "failed" } },
      { completed_at: "" },
      { retry_after_seconds: -1 },
      { retry_after_seconds: 3601 },
      { assistant_hint: [] },
    ]) {
      expect(() => parseConnectorApproval(connectorApproval(overrides))).toThrow(/Invalid connector approval/);
    }
    expect(
      parseConnectorApproval(
        connectorApproval({
          token_id: 7,
          token_name: "codex",
          title: "Review command",
          summary: "Runs one command.",
          preview: { command: "uptime" },
          input: { command: "uptime" },
          reason: "Check health",
          display_text: "Ready",
          error: "",
          completed_at: "2026-09-16T00:01:00Z",
          retry_after_seconds: 3,
          assistant_hint: "Wait for completion.",
        }),
      ),
    ).toMatchObject({ token_id: 7, preview: { command: "uptime" } });
  });

  it("validates every typed Vault approval context field", () => {
    const item = {
      item_id: 11,
      name: "API_TOKEN",
      source_project_id: 4,
      value_version: 2,
      metadata_revision: 3,
      replace_existing: false,
      binding_id: 8,
      binding_revision: 5,
    };
    const context = {
      ...vaultApproval().approval_context,
      action_name: "restart_session_with_environment",
      target_id: 2,
      profile_id: 3,
      expected_session_id: 4,
      connector_kind: "ssh",
      items: [item],
    };
    expect(parseVaultApproval(vaultApproval({ action_name: "restart_session_with_environment", approval_context: context }))).toMatchObject(
      { approval_context: context },
    );

    const invalidContexts = [
      null,
      [],
      { target_id: 0 },
      { profile_id: "3" },
      { expected_session_id: -1 },
      { connector_kind: "" },
      { items: {} },
      { items: [null] },
      { items: [{ ...item, item_id: 0 }] },
      { items: [{ ...item, name: "" }] },
      { items: [{ ...item, source_project_id: 0 }] },
      { items: [{ ...item, value_version: 0 }] },
      { items: [{ ...item, metadata_revision: 0 }] },
      { items: [{ ...item, replace_existing: "false" }] },
      { items: [{ ...item, binding_id: 0 }] },
      { items: [{ ...item, binding_revision: 0 }] },
    ];
    for (const approvalContext of invalidContexts) {
      expect(() => parseVaultApproval(vaultApproval({ approval_context: approvalContext }))).toThrow(/Invalid Vault approval/);
    }
    expect(() => parseVaultApproval(vaultApproval({ approval_context: {} }))).toThrow(/Invalid Vault approval/);
    expect(parseVaultApproval(vaultApproval({ status: "completed", approval_context_hash: "", approval_context: {} }))).toMatchObject({
      approval_context: {},
    });
    expect(
      parseVaultApproval(vaultApproval({ status: "completed", approval_context_hash: "", approval_context: undefined })),
    ).toMatchObject({ status: "completed" });
    expect(() => parseVaultApproval(vaultApproval({ status: "completed", approval_context_hash: "", approval_context: [] }))).toThrow(
      /Invalid Vault approval/,
    );

    for (const approvalContext of [
      { ...vaultApproval().approval_context, action_name: "restart_session_with_environment" },
      { ...vaultApproval().approval_context, token_id: 99 },
      { ...vaultApproval().approval_context, project_id: 99 },
    ]) {
      expect(() => parseVaultApproval(vaultApproval({ approval_context: approvalContext }))).toThrow(/Invalid Vault approval/);
    }

    const expected = { id: 1, tokenID: 7, projectID: 3, actionName: "generate_item", statuses: ["approval_pending"] };
    expect(parseVaultApproval(vaultApproval(), expected)).toMatchObject({ id: 1, token_id: 7, project_id: 3 });
    for (const mismatch of [
      { ...expected, tokenID: 8 },
      { ...expected, projectID: 4 },
      { ...expected, actionName: "restart_session_with_environment" },
      { ...expected, statuses: ["completed"] },
    ]) {
      expect(() => parseVaultApproval(vaultApproval(), mismatch)).toThrow(/Invalid Vault approval/);
    }
  });

  it("rejects malformed Vault approval envelope fields", () => {
    const invalid = [
      { status: 1 },
      { status: "unknown" },
      { token_id: 0 },
      { token_name: "" },
      { project_id: 0 },
      { project_name: "" },
      { project_slug: "" },
      { action_name: "" },
      { source: "" },
      { input: [] },
      { reason: 7 },
      { idempotency_key: "" },
      { created_at: "" },
      { expires_at: "" },
      { updated_at: "" },
    ];
    for (const overrides of invalid) {
      expect(() => parseVaultApproval(vaultApproval(overrides))).toThrow(/Invalid Vault approval/);
    }
    expect(
      parseVaultApproval(vaultApproval({ status: "completed", approval_context_hash: "", reason: undefined }), {
        id: 1,
        statuses: ["completed"],
      }),
    ).toMatchObject({ status: "completed" });
  });

  it("rejects malformed console sessions without activating a socket", () => {
    expect(() => consoleSessions([{ id: 0 }])).toThrow(/Invalid console session/);
    expect(consoleSessions([{ id: 1, status: "active" }])).toHaveLength(1);
  });
});

function connectorApproval(overrides = {}) {
  return {
    id: 1,
    status: "approval_pending",
    target_id: 2,
    target_name: "Fixture",
    target_ref: "fixture:2:3",
    profile_id: 3,
    profile_label: "default",
    connector_kind: "fixture",
    action_name: "read",
    approval_context_hash: "context-hash",
    retry_policy: { class: "read_only", guidance: "Safe to retry." },
    created_at: "2026-09-16T00:00:00Z",
    ...overrides,
  };
}

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
    approval_context: {
      schema: "vault-action-v3",
      action_name: "generate_item",
      token_id: 7,
      project_id: 3,
      workspace_id: "fixture-workspace",
      runtime_instance_id: "fixture-runtime",
      capability_name: "vault_item_generate",
      execution_rule: "prompt",
      input_hash: "input-hash",
      project_scope_hash: "scope-hash",
    },
    approval_context_hash: "context-hash",
    idempotency_key: "fixture-key",
    created_at: "2026-09-16T00:00:00Z",
    expires_at: "2026-09-16T00:15:00Z",
    updated_at: "2026-09-16T00:00:00Z",
    ...overrides,
  };
}
