import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiGet, apiPost, apiPut } from "../../lib/api";
import { HistoryRetentionPanel } from "./history-retention-panel";
import { PasswordSettingsPanel } from "./password-settings-panel";

vi.mock("../../lib/api", () => ({
  apiDelete: vi.fn(),
  apiGet: vi.fn(),
  apiPost: vi.fn(),
  apiPut: vi.fn(),
}));

describe("settings panels", () => {
  beforeEach(() => {
    apiGet.mockReset();
    apiPost.mockReset();
    apiPut.mockReset();
  });

  it("keeps password fields available after failure and clears them after success", async () => {
    const user = userEvent.setup();
    apiPost.mockRejectedValueOnce(new Error("Current password is invalid")).mockResolvedValueOnce({ ok: true });
    render(<PasswordSettingsPanel />);

    await user.type(screen.getByLabelText("Current password"), "CurrentPassword123");
    await user.type(screen.getByLabelText("New password"), "ReplacementPassword456");
    await user.type(screen.getByLabelText("Confirm new password"), "ReplacementPassword456");
    await user.click(screen.getByRole("button", { name: "Change password" }));
    expect(await screen.findByText("Current password is invalid")).toBeVisible();
    expect(screen.getByLabelText("Current password")).toHaveValue("CurrentPassword123");

    await user.click(screen.getByRole("button", { name: "Change password" }));
    expect(await screen.findByText("Database password changed. Future unlocks and new backups use the new password.")).toBeVisible();
    expect(screen.getByLabelText("Current password")).toHaveValue("");
    expect(screen.getByLabelText("New password")).toHaveValue("");
  });

  it("loads and saves retention settings without coupling to the settings page", async () => {
    const user = userEvent.setup();
    apiGet.mockResolvedValue({ history_days: 7, audit_days: 14, console_days: 3, message_days: 2 });
    apiPut.mockResolvedValue({ history_days: 30, audit_days: 14, console_days: 3, message_days: 2 });
    render(<HistoryRetentionPanel />);

    await waitFor(() => expect(screen.getByLabelText("Command history days")).toHaveValue(7));
    await user.clear(screen.getByLabelText("Command history days"));
    await user.type(screen.getByLabelText("Command history days"), "30");
    await user.click(screen.getByRole("button", { name: "Save retention" }));

    expect(apiPut).toHaveBeenCalledWith("/api/settings/retention", {
      history_days: 30,
      audit_days: 14,
      console_days: 3,
      message_days: 2,
    });
    expect(await screen.findByText("Retention settings saved and cleanup ran.")).toBeVisible();
  });
});
