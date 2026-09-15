import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiDelete, apiGet, apiPost, apiPut } from "../../lib/api";
import { BackupRetentionPanel } from "./backup-retention-panel";
import { DatabaseSettingsPanel } from "./database-settings-panel";
import { HistoryLabelsPanel } from "./history-labels-panel";
import { HistoryRetentionPanel } from "./history-retention-panel";

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
    apiPut.mockReset();
  });

  it("locks history retention fields while a save is pending", async () => {
    const save = deferred();
    const values = { history_days: 30, audit_days: 30, console_days: 7, message_days: 7 };
    apiGet.mockResolvedValue(values);
    apiPut.mockReturnValue(save.promise);
    render(<HistoryRetentionPanel />);

    const field = await screen.findByLabelText("Command history days");
    fireEvent.click(screen.getByRole("button", { name: "Save retention" }));
    expect(field).toBeDisabled();
    fireEvent.change(field, { target: { value: "60" } });
    expect(field).toHaveValue(30);
    expect(apiPut).toHaveBeenCalledWith("/api/settings/retention", values);
    await act(async () => save.resolve(values));
    expect(field).toBeEnabled();
    expect(field).toHaveValue(30);
  });

  it("keeps backup retention busy until its post-save reload completes", async () => {
    const save = deferred();
    const reloadStorage = deferred();
    const reloadPolicy = deferred();
    let storageReads = 0;
    apiGet.mockImplementation((path) => {
      if (path.endsWith("/storage")) return ++storageReads === 1 ? Promise.resolve(storage(1024)) : reloadStorage.promise;
      if (path.endsWith("/retention")) return storageReads === 1 ? Promise.resolve(policy(10)) : reloadPolicy.promise;
      return Promise.reject(new Error(`Unexpected GET ${path}`));
    });
    apiPost.mockResolvedValue({ keep_latest: 10, retain_count: 1, retain_bytes: 1024, delete_count: 0, delete_bytes: 0 });
    apiPut.mockReturnValue(save.promise);
    render(<BackupRetentionPanel provider={{ id: 1 }} />);

    await screen.findByDisplayValue("10");
    fireEvent.click(screen.getByRole("button", { name: "Preview" }));
    await screen.findByText(/Keep 1/);
    fireEvent.click(screen.getByRole("button", { name: "Save policy" }));
    expect(screen.getByRole("checkbox", { name: "Automatic retention" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Refresh storage and retention" })).toBeDisabled();
    await act(async () => save.resolve({ policy: policy(10), deleted_count: 0 }));
    expect(screen.getByRole("button", { name: "Refresh storage and retention" })).toBeDisabled();
    await act(async () => {
      reloadStorage.resolve(storage(2048));
      reloadPolicy.resolve(policy(10));
    });
    expect(screen.getByRole("checkbox", { name: "Automatic retention" })).toBeEnabled();
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

  it("ignores a backup retention save returned for an older provider", async () => {
    const save = deferred();
    apiGet.mockImplementation((path) => {
      if (path === "/api/backup/providers/1/storage") return Promise.resolve(storage(1024));
      if (path === "/api/backup/providers/1/retention") return Promise.resolve(policy(10));
      if (path === "/api/backup/providers/2/storage") return Promise.resolve(storage(2048));
      if (path === "/api/backup/providers/2/retention") return Promise.resolve(policy(20));
      return Promise.reject(new Error(`Unexpected GET ${path}`));
    });
    apiPost.mockResolvedValue({ keep_latest: 10, retain_count: 1, retain_bytes: 1024, delete_count: 0, delete_bytes: 0 });
    apiPut.mockReturnValue(save.promise);
    const { rerender } = render(<BackupRetentionPanel provider={{ id: 1 }} />);

    await screen.findByDisplayValue("10");
    fireEvent.click(screen.getByRole("button", { name: "Preview" }));
    await screen.findByText(/Keep 1/);
    fireEvent.click(screen.getByRole("button", { name: "Save policy" }));
    rerender(<BackupRetentionPanel provider={{ id: 2 }} />);
    expect(await screen.findByDisplayValue("20")).toBeVisible();
    await act(async () => save.resolve({ policy: policy(10), deleted_count: 0 }));

    expect(screen.getByDisplayValue("20")).toBeVisible();
    expect(screen.queryByText(/Automatic retention enabled/)).not.toBeInTheDocument();
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
