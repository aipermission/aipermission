import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { ConnectorActionApprovalDialog } from "../../components/console/connector-action-approval-dialog";
import type { ConnectorApproval } from "../../lib/gateway-contracts/security-contracts.ts";
import type { ApprovalDialogAction } from "../../components/console/use-connector-approval-dialog.ts";

const approval: ConnectorApproval = {
  id: 42,
  target_id: 7,
  profile_id: 11,
  status: "approval_pending",
  approval_context_hash: "reviewed-context",
  retry_policy: { class: "read_only", guidance: "Read only query" },
  connector_kind: "postgres",
  target_name: "Application database",
  profile_label: "Read only",
  target_ref: "postgres:7:11",
  token_name: "codex",
  action_name: "query_readonly",
  reason: "Inspect recent jobs",
  input: { query: "select 1" },
  preview: { query: "select 1", mode: "read only" },
  created_at: "2026-08-11T00:00:00Z",
};

function renderDialog(action: ApprovalDialogAction = { state: "idle", error: "" }) {
  const handlers = {
    onNoteChange: vi.fn(),
    onRun: vi.fn(),
    onDecline: vi.fn(),
    onClose: vi.fn(),
  };
  render(<ConnectorActionApprovalDialog approval={approval} note="" action={action} {...handlers} />);
  return handlers;
}

describe("ConnectorActionApprovalDialog", () => {
  it("renders no decision or payload when no approval is selected", () => {
    render(
      <ConnectorActionApprovalDialog
        approval={null}
        note=""
        action={{ state: "idle", error: null }}
        onNoteChange={vi.fn()}
        onRun={vi.fn()}
        onDecline={vi.fn()}
        onClose={vi.fn()}
      />,
    );
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Run" })).not.toBeInTheDocument();
  });

  it("handles absent optional context and payload without inventing a preview", () => {
    render(
      <ConnectorActionApprovalDialog
        approval={{
          ...approval,
          created_at: "invalid",
          token_name: undefined,
          reason: undefined,
          input: undefined,
          preview: undefined,
          title: "Reviewed title",
          summary: "Reviewed summary",
        }}
        note=""
        action={{ state: "error", error: "Retry requires review" }}
        onNoteChange={vi.fn()}
        onRun={vi.fn()}
        onDecline={vi.fn()}
        onClose={vi.fn()}
      />,
    );
    expect(screen.getByText("Reviewed title")).toBeVisible();
    expect(screen.getByText("Reviewed summary")).toBeVisible();
    expect(screen.getByText("Retry requires review")).toBeVisible();
    expect(screen.getByText("No structured preview was provided.")).toBeVisible();
  });

  it("edits the reviewed note only before a decision has been admitted", async () => {
    const user = userEvent.setup();
    const handlers = renderDialog();
    await user.type(screen.getByRole("textbox", { name: "Decline note" }), "x");
    expect(handlers.onNoteChange).toHaveBeenCalledWith("x");
  });

  it("keeps Run and Decline as explicit user decisions", async () => {
    const user = userEvent.setup();
    const handlers = renderDialog();

    await user.click(screen.getByRole("button", { name: "Run" }));
    await user.click(screen.getByRole("button", { name: "Decline" }));

    expect(handlers.onRun).toHaveBeenCalledOnce();
    expect(handlers.onDecline).toHaveBeenCalledOnce();
  });

  it("replaces executable decisions with acknowledgement after context becomes stale", async () => {
    const user = userEvent.setup();
    const handlers = renderDialog({ state: "stale", error: "Approval context changed. Review a fresh request." });

    expect(screen.queryByRole("button", { name: "Run" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Decline" })).not.toBeInTheDocument();
    expect(screen.getByText("Approval context changed. Review a fresh request.")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "OK" }));
    expect(handlers.onClose).toHaveBeenCalledOnce();
  });

  it("shows bounded loading and no-preview states", () => {
    const { rerender } = render(
      <ConnectorActionApprovalDialog
        approval={{ ...approval, preview: {} }}
        note=""
        action={{ state: "loading", error: "" }}
        onNoteChange={vi.fn()}
        onRun={vi.fn()}
        onDecline={vi.fn()}
        onClose={vi.fn()}
      />,
    );
    expect(screen.getByText("Loading the exact approval preview...")).toBeVisible();
    expect(screen.getByText("Loading redacted input...")).toBeVisible();

    rerender(
      <ConnectorActionApprovalDialog
        approval={{ ...approval, preview: {} }}
        note=""
        action={{ state: "idle", error: "" }}
        onNoteChange={vi.fn()}
        onRun={vi.fn()}
        onDecline={vi.fn()}
        onClose={vi.fn()}
      />,
    );
    expect(screen.getByText("No structured preview was provided.")).toBeVisible();
  });

  it("disables repeated decisions while a request is running", () => {
    renderDialog({ state: "running", error: "" });
    expect(screen.getByRole("button", { name: "Running..." })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Decline" })).toBeDisabled();
  });

  it.each(["running", "declining"] as const)("blocks every dismissal path and note edits while %s", async (state) => {
    const user = userEvent.setup();
    const handlers = renderDialog({ state, error: "" });
    expect(screen.getByRole("button", { name: "Close dialog" })).toBeDisabled();
    expect(screen.getByRole("textbox", { name: "Decline note" })).toBeDisabled();
    await user.keyboard("{Escape}");
    fireEvent.pointerDown(screen.getByTestId("dialog-overlay"));
    expect(handlers.onClose).not.toHaveBeenCalled();
  });
});
