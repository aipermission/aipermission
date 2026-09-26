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
    vi.restoreAllMocks();
    vi.mocked(apiGet).mockReset();
    vi.mocked(apiPost).mockReset();
    vi.mocked(apiPut).mockReset();
  });

  it("keeps password fields available after failure and clears them after success", async () => {
    const user = userEvent.setup();
    vi.mocked(apiPost).mockRejectedValueOnce(new Error("Current password is invalid")).mockResolvedValueOnce({ ok: true });
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
    vi.mocked(apiGet).mockResolvedValue({ history_days: 7, audit_days: 14, console_days: 3, message_days: 2 });
    vi.mocked(apiPut).mockResolvedValue({ history_days: 30, audit_days: 14, console_days: 3, message_days: 2 });
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

  it("does not submit default retention values after load failure and retries before saving", async () => {
    const user = userEvent.setup();
    vi.mocked(apiGet).mockRejectedValueOnce(new Error("Retention unavailable")).mockResolvedValueOnce({
      history_days: 7,
      audit_days: 14,
      console_days: 3,
      message_days: 2,
    });
    render(<HistoryRetentionPanel />);

    expect(await screen.findByText("Retention unavailable")).toBeVisible();
    expect(screen.getByRole("button", { name: "Save retention" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "Save retention" }));
    expect(apiPut).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "Save retention" })).toBeEnabled());
    expect(screen.getByLabelText("Command history days")).toHaveValue(7);
  });

  it("confirms and reports a manual retention purge", async () => {
    const user = userEvent.setup();
    vi.mocked(apiGet).mockResolvedValue({ history_days: 7, audit_days: 14, console_days: 3, message_days: 2 });
    vi.mocked(apiPost).mockResolvedValue({ deleted: 4 });
    vi.spyOn(window, "confirm").mockReturnValue(true);
    render(<HistoryRetentionPanel />);

    await screen.findByLabelText("Command history days");
    await user.click(screen.getByRole("button", { name: "Purge history older than 30 days" }));

    expect(apiPost).toHaveBeenCalledWith("/api/settings/retention/purge", { target: "history", days: 30 });
    expect(await screen.findByText("Deleted 4 history records.")).toBeVisible();
  });

  it("retains editable settings after an invalid save response and permits retry", async () => {
    const user = userEvent.setup();
    const settings = { history_days: 7, audit_days: 14, console_days: 3, message_days: 2 };
    vi.mocked(apiGet).mockResolvedValue(settings);
    vi.mocked(apiPut).mockResolvedValueOnce({ history_days: "7" }).mockResolvedValueOnce(settings);
    render(<HistoryRetentionPanel />);
    await waitFor(() => expect(screen.getByRole("button", { name: "Save retention" })).toBeEnabled());
    await user.click(screen.getByRole("button", { name: "Save retention" }));
    expect(await screen.findByText("Retention settings response is invalid.")).toBeVisible();
    expect(screen.getByLabelText("Audit log days")).toHaveValue(14);
    await user.click(screen.getByRole("button", { name: "Save retention" }));
    expect(await screen.findByText("Retention settings saved and cleanup ran.")).toBeVisible();
  });

  it("does not report malformed purge counts as a successful deletion", async () => {
    const user = userEvent.setup();
    vi.mocked(apiGet).mockResolvedValue({ history_days: 7, audit_days: 14, console_days: 3, message_days: 2 });
    vi.mocked(apiPost).mockResolvedValue({ deleted: -1 });
    vi.spyOn(window, "confirm").mockReturnValue(true);
    render(<HistoryRetentionPanel />);
    await user.click(screen.getByRole("button", { name: "Purge history older than 30 days" }));
    expect(await screen.findByText("Retention purge response is invalid.")).toBeVisible();
    expect(screen.queryByText(/Deleted /)).not.toBeInTheDocument();
  });

  it("does not dispatch a purge when confirmation is canceled", async () => {
    const user = userEvent.setup();
    vi.mocked(apiGet).mockResolvedValue({ history_days: 0, audit_days: 0, console_days: 0, message_days: 0 });
    vi.spyOn(window, "confirm").mockReturnValue(false);
    render(<HistoryRetentionPanel />);
    await user.click(screen.getByRole("button", { name: "Purge messages older than 7 days" }));
    expect(apiPost).not.toHaveBeenCalled();
  });

  it("aborts the owned retention read when the panel unmounts", () => {
    vi.mocked(apiGet).mockReturnValue(new Promise(() => {}));
    const { unmount } = render(<HistoryRetentionPanel />);
    const options: unknown = vi.mocked(apiGet).mock.calls[0]?.[1];
    if (!options || typeof options !== "object" || !("signal" in options) || !(options.signal instanceof AbortSignal))
      throw new Error("Expected an owned retention read.");
    expect(options.signal.aborted).toBe(false);
    unmount();
    expect(options.signal.aborted).toBe(true);
  });
});
