import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { VaultActionApprovalDialog } from "./vault-action-approval-dialog";

const approval = {
  id: 91,
  token_name: "codex",
  project_name: "My Project",
  action_name: "generate_item",
  reason: "Create a deployment token",
  input: { name: "DEPLOY_TOKEN" },
  created_at: "2026-08-11T00:00:00Z",
  expires_at: "2026-08-11T00:10:00Z",
  approval_context: {},
};

function renderDialog(overrides = {}) {
  const handlers = { onNoteChange: vi.fn(), onRun: vi.fn(), onDecline: vi.fn(), onClose: vi.fn() };
  render(
    <VaultActionApprovalDialog
      approval={{ ...approval, ...overrides.approval }}
      note=""
      action={overrides.action || { state: "idle", error: "" }}
      {...handlers}
    />,
  );
  return handlers;
}

describe("VaultActionApprovalDialog", () => {
  it("keeps generation decisions explicit without exposing a value", async () => {
    const user = userEvent.setup();
    const handlers = renderDialog();

    expect(screen.getByText(/generated value stays hidden/i)).toBeVisible();
    expect(screen.queryByText(/secret value/i)).not.toBeInTheDocument();
    await user.type(screen.getByRole("textbox", { name: "Decision note" }), "Approved locally");
    await user.click(screen.getByRole("button", { name: "Run" }));
    expect(handlers.onNoteChange).toHaveBeenCalled();
    expect(handlers.onRun).toHaveBeenCalledOnce();
  });

  it("shows exact environment assignments and makes stale approvals acknowledgement-only", async () => {
    const user = userEvent.setup();
    const handlers = renderDialog({
      approval: {
        action_name: "restart_session_with_environment",
        approval_context: {
          connector_kind: "ssh",
          target_id: 4,
          profile_id: 8,
          expected_session_id: 12,
          items: [{ item_id: 7, source_project_id: 2, name: "DEPLOY_TOKEN", replace_existing: true }],
        },
      },
      action: { state: "stale", error: "Approval context changed." },
    });

    expect(screen.getByText("DEPLOY_TOKEN")).toBeVisible();
    expect(screen.getByText(/overwrites existing shell value/i)).toBeVisible();
    expect(screen.queryByRole("button", { name: "Run" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "OK" }));
    expect(handlers.onClose).toHaveBeenCalledOnce();
  });
});
