import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { scopedUICookieName } from "../lib/ui-cookie";
import { LocalActionReconciliationDialog } from "./local-action-reconciliation-dialog";
import { LocalActionRetryPanel } from "./settings/local-action-retry-panel";

describe("local connector action reconciliation", () => {
  it("keeps an unknown outcome protected until the operator explicitly starts a new attempt", async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();
    render(<LocalActionReconciliationDialog value={{ requestID: 91, assistantHint: "Inspect external state first." }} onClose={onClose} />);

    expect(screen.getByText(/may repeat an operation that already completed/i)).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Keep protected" }));
    expect(onClose).toHaveBeenLastCalledWith(false);

    await user.click(screen.getByRole("button", { name: "Start new attempt" }));
    expect(onClose).toHaveBeenLastCalledWith(true);
  });

  it("requires confirmation before removing a retained retry identity from Settings", async () => {
    const user = userEvent.setup();
    document.cookie = `${scopedUICookieName("aipermission_workspace")}=test-workspace; Path=/`;
    window.localStorage.setItem(
      "aipermission.local-action-retry.v2.test-workspace",
      JSON.stringify({
        version: 2,
        entries: {
          signature: {
            key: "retry-key",
            state: "outcome_unknown",
            request_id: 91,
            created_at: "2026-09-04T10:00:00Z",
            updated_at: "2026-09-04T10:00:00Z",
          },
        },
      }),
    );
    render(<LocalActionRetryPanel />);

    expect(screen.getByText("Outcome unknown")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Mark retry identity reconciled" }));
    expect(screen.getByText(/next identical action will be a new external attempt/i)).toBeVisible();
    expect(window.localStorage.getItem("aipermission.local-action-retry.v2.test-workspace")).not.toBeNull();

    await user.click(screen.getByRole("button", { name: "Mark reconciled" }));
    expect(await screen.findByText("No unresolved local connector attempts.")).toBeVisible();
    expect(window.localStorage.getItem("aipermission.local-action-retry.v2.test-workspace")).toBeNull();
  });
});
