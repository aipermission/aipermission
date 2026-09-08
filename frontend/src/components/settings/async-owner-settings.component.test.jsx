import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiDelete, apiGet, apiPost } from "../../lib/api";
import { BackupRetentionPanel } from "./backup-retention-panel";
import { DatabaseSettingsPanel } from "./database-settings-panel";
import { HistoryLabelsPanel } from "./history-labels-panel";

vi.mock("../../lib/api", () => ({
  apiDelete: vi.fn(),
  apiGet: vi.fn(),
  apiPost: vi.fn(),
  apiPut: vi.fn(),
}));

function deferred() {
  let resolve;
  const promise = new Promise((next) => {
    resolve = next;
  });
  return { promise, resolve };
}

function storage(usedBytes) {
  return { used_bytes: usedBytes, quota_enabled: false, pending_deletions: 0 };
}

function policy(keepLatest) {
  return { enabled: true, keep_latest: keepLatest };
}

describe("settings async owners", () => {
  beforeEach(() => {
    apiDelete.mockReset();
    apiGet.mockReset();
    apiPost.mockReset();
  });

  it("ignores backup retention data returned for an older provider", async () => {
    const olderStorage = deferred();
    const olderPolicy = deferred();
    apiGet.mockImplementation((path) => {
      if (path === "/api/backup/providers/1/storage") return olderStorage.promise;
      if (path === "/api/backup/providers/1/retention") return olderPolicy.promise;
      if (path === "/api/backup/providers/2/storage") return Promise.resolve(storage(2048));
      if (path === "/api/backup/providers/2/retention") return Promise.resolve(policy(20));
      return Promise.reject(new Error(`Unexpected GET ${path}`));
    });
    const { rerender } = render(<BackupRetentionPanel provider={{ id: 1 }} />);

    rerender(<BackupRetentionPanel provider={{ id: 2 }} />);
    expect(await screen.findByDisplayValue("20")).toBeVisible();
    await act(async () => {
      olderStorage.resolve(storage(1024));
      olderPolicy.resolve(policy(10));
    });

    expect(screen.getByDisplayValue("20")).toBeVisible();
    expect(screen.queryByDisplayValue("10")).not.toBeInTheDocument();
  });

  it("keeps the database rename form usable after an API failure", async () => {
    apiPost.mockRejectedValue(new Error("rename refused"));
    render(<DatabaseSettingsPanel databaseName="Default" />);

    fireEvent.change(screen.getByLabelText("Database name"), { target: { value: "Renamed" } });
    fireEvent.change(screen.getAllByLabelText("Current database password").at(-1), { target: { value: "SecretPassword123" } });
    fireEvent.click(screen.getByRole("button", { name: "Rename database" }));

    expect(await screen.findByText("rename refused")).toBeVisible();
    expect(screen.getByLabelText("Database name")).toHaveValue("Renamed");
    expect(screen.getAllByLabelText("Current database password").at(-1)).toHaveValue("");
  });

  it("deletes the selected history label and refreshes its collection", async () => {
    const user = userEvent.setup();
    apiGet.mockResolvedValueOnce([{ id: 7, name: "incident" }]).mockResolvedValueOnce([]);
    apiDelete.mockResolvedValue({});
    render(<HistoryLabelsPanel />);

    await user.selectOptions(await screen.findByLabelText("History label"), "7");
    await user.click(screen.getByRole("button", { name: "Delete label" }));
    await user.click(screen.getByRole("button", { name: "Delete label" }));

    await waitFor(() => expect(apiDelete).toHaveBeenCalledWith("/api/history-labels/7"));
    expect(await screen.findByText('Deleted history label "incident".')).toBeVisible();
    expect(screen.getByText("No labels yet. Add labels from a history detail.")).toBeVisible();
  });
});
