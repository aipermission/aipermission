import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { ConnectorActionApprovalDialog } from "./connector-action-approval-dialog";

const approval = {
  id: 42,
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

function renderDialog(action = { state: "idle", error: "" }) {
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
  it("keeps Run and Decline as explicit user decisions", async () => {
    const user = userEvent.setup();
    const handlers = renderDialog();

    await user.click(screen.getByRole("button", { name: "Run", exact: true }));
    await user.click(screen.getByRole("button", { name: "Decline", exact: true }));

    expect(handlers.onRun).toHaveBeenCalledOnce();
    expect(handlers.onDecline).toHaveBeenCalledOnce();
  });

  it("replaces executable decisions with acknowledgement after context becomes stale", async () => {
    const user = userEvent.setup();
    const handlers = renderDialog({ state: "stale", error: "Approval context changed. Review a fresh request." });

    expect(screen.queryByRole("button", { name: "Run", exact: true })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Decline", exact: true })).not.toBeInTheDocument();
    expect(screen.getByText("Approval context changed. Review a fresh request.")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "OK", exact: true }));
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
});
