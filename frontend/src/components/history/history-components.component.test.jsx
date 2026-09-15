import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { HistoryDialog, StatusBadge, retryPolicyGuidance } from "./history-components";

describe("history outcome uncertainty", () => {
  it("uses a visible warning status and persisted retry guidance", () => {
    const guidance = "Inspect the object metadata before submitting another mutation.";
    render(
      <>
        <StatusBadge status="outcome_unknown" />
        <HistoryDialog
          item={{
            id: 43,
            status: "outcome_unknown",
            activity_type: "action",
            action_name: "delete_object",
            target_name: "object-store",
            created_at: "2026-09-03T09:00:00Z",
            retry_policy_json: JSON.stringify({ class: "conditional", guidance }),
          }}
          onClose={() => {}}
          onAttachLabel={async () => {}}
          onDetachLabel={async () => {}}
        />
      </>,
    );

    expect(screen.getAllByText("outcome_unknown").length).toBeGreaterThan(0);
    expect(screen.getByText(/Remote outcome unknown/)).toBeInTheDocument();
    expect(screen.getByText(new RegExp(guidance))).toBeInTheDocument();
  });

  it("fails closed when persisted guidance cannot be decoded", () => {
    expect(retryPolicyGuidance({ retry_policy_json: "not-json" })).toMatch(/Inspect the target state/);
  });

  it("shows persisted retry guidance after a failed precondition", () => {
    const guidance = "Read fresh metadata and submit a new precondition.";
    render(
      <HistoryDialog
        item={{
          id: 44,
          status: "failed",
          activity_type: "action",
          action_name: "delete_object",
          target_name: "object-store",
          created_at: "2026-09-03T09:00:00Z",
          output_json: JSON.stringify({ code: "precondition_failed" }),
          retry_policy_json: JSON.stringify({ class: "conditional", guidance }),
        }}
        onClose={() => {}}
        onAttachLabel={async () => {}}
        onDetachLabel={async () => {}}
      />,
    );

    expect(screen.getByText(/Precondition failed/)).toBeInTheDocument();
    expect(screen.getByText(new RegExp(guidance))).toBeInTheDocument();
  });
});

it("allows history label suggestions to be selected with the keyboard", async () => {
  const user = userEvent.setup();
  const onAttachLabel = vi.fn().mockResolvedValue(null);
  render(
    <HistoryDialog
      item={{ id: 42, status: "completed", labels: [], target_name: "Test target", created_at: "2026-09-01T00:00:00Z" }}
      labels={[{ id: 1, name: "Investigate" }]}
      onClose={vi.fn()}
      onAttachLabel={onAttachLabel}
      onDetachLabel={vi.fn()}
    />,
  );

  const input = screen.getByRole("textbox", { name: "Add history label" });
  await user.click(input);
  await user.type(input, "Invest");
  await user.tab();
  expect(screen.getByText("Investigate").closest("button")).toHaveFocus();
  await user.keyboard("{Enter}");

  await waitFor(() => expect(onAttachLabel).toHaveBeenCalledWith(42, { name: "Investigate" }));
});
