import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiDelete, apiGet } from "../../lib/api";
import { HistoryLabelsPanel } from "./history-labels-panel";

vi.mock("../../lib/api", () => ({ apiGet: vi.fn(), apiDelete: vi.fn() }));

describe("history label settings", () => {
  beforeEach(() => { vi.mocked(apiGet).mockReset(); vi.mocked(apiDelete).mockReset(); });

  it.each([{}, [{ id: "7", name: "incident" }], [{ id: 0, name: "incident" }], [{ id: 7, name: null }]])("rejects malformed label collections %j", async (value) => {
    vi.mocked(apiGet).mockResolvedValue(value);
    render(<HistoryLabelsPanel />);
    expect(await screen.findByText("History label response is invalid.")).toBeVisible();
    expect(screen.getByRole("button", { name: "Delete label" })).toBeDisabled();
  });

  it("treats the backend null collection as an empty list", async () => {
    vi.mocked(apiGet).mockResolvedValue(null);
    render(<HistoryLabelsPanel />);
    expect(await screen.findByText("No labels yet. Add labels from a history detail.")).toBeVisible();
  });

  it("preserves selection after a failed delete and refreshes after retry", async () => {
    const user = userEvent.setup();
    vi.mocked(apiGet).mockResolvedValueOnce([{ id: 7, name: "incident" }]).mockResolvedValueOnce([]);
    vi.mocked(apiDelete).mockRejectedValueOnce(new Error("Delete refused")).mockResolvedValueOnce({});
    render(<HistoryLabelsPanel />);
    await waitFor(() => expect(screen.getByLabelText("History label")).toBeEnabled());
    await user.selectOptions(screen.getByLabelText("History label"), "7");
    await user.click(screen.getByRole("button", { name: "Delete label" }));
    await user.click(screen.getByRole("button", { name: "Delete label" }));
    expect(await screen.findAllByText("Delete refused")).toHaveLength(2);
    await user.click(screen.getByRole("button", { name: "Delete label" }));
    expect(await screen.findByText('Deleted history label "incident".')).toBeVisible();
    expect(apiDelete).toHaveBeenCalledTimes(2);
  });
});
