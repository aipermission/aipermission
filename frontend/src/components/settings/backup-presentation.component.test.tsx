import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiDelete, apiDownload, apiGet, apiPost, apiPut } from "../../lib/api";
import { BackupProviderPanel } from "./backup-provider-panel";
import { BackupProviderDialogs } from "./backup-provider-dialogs";
import { BackupRecordDialogs } from "./backup-record-dialogs";
import { useBackupProviderState } from "./use-backup-provider-state";
import { isBackupProvider, isBackupRecord } from "./backup-contracts";

vi.mock("../../lib/api", () => ({ apiGet: vi.fn(), apiPost: vi.fn(), apiPut: vi.fn(), apiDelete: vi.fn(), apiDownload: vi.fn() }));
const database = { data: { database_name: "My database", database_size_bytes: 1024 } };
const provider = { id: 1, name: "My backups", provider_type: "aipermission_backup", status: "active", has_secret: true, public: { base_url: "https://backups.example.com" } };
const records = [{ id: 2, filename: "new.aipdb", size_bytes: 1024 }, { id: 1, filename: "old.aipdb", size_bytes: 512 }];

function BackupHarness() {
  const state = useBackupProviderState(database);
  return <><BackupProviderPanel state={state} /><BackupProviderDialogs state={state} database={database} /><BackupRecordDialogs state={state} /></>;
}

describe("typed backup presentation", () => {
  beforeEach(() => {
    vi.mocked(apiGet).mockReset(); vi.mocked(apiPost).mockReset(); vi.mocked(apiPut).mockReset(); vi.mocked(apiDelete).mockReset(); vi.mocked(apiDownload).mockReset();
    vi.mocked(apiGet).mockImplementation((path) => {
      if (path === "/api/backup/providers/catalog") return Promise.resolve({ items: [{ provider_type: "aipermission_backup", label: "AIPermission Backup" }] });
      if (path === "/api/backup/providers") return Promise.resolve({ items: [provider] });
      if (path.endsWith("/records")) return Promise.resolve({ items: records });
      if (path.endsWith("/storage")) return Promise.resolve({ used_bytes: 1536, pending_deletions: 0, quota_enabled: false });
      if (path.endsWith("/retention")) return Promise.resolve({ enabled: false });
      return Promise.reject(new Error(`Unexpected GET ${path}`));
    });
  });

  it("renders provider metadata and opens its isolated edit form", async () => {
    const user = userEvent.setup();
    render(<BackupHarness />);
    await screen.findByText("My backups");
    expect(screen.getByText("https://backups.example.com")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Edit" }));
    const dialog = screen.getByRole("dialog", { name: "Edit backup provider" });
    expect(within(dialog).getByLabelText("Name")).toHaveValue("My backups");
    expect(within(dialog).getByLabelText(/Service token/)).toHaveValue("");
    expect(within(dialog).getByLabelText("Provider type")).toBeDisabled();
    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("opens the upload confirmation with the current database size", async () => {
    const user = userEvent.setup();
    render(<BackupHarness />);
    await screen.findByText("My backups");
    await user.click(screen.getByRole("button", { name: "Upload" }));
    const dialog = screen.getByRole("dialog", { name: "Upload encrypted backup" });
    expect(within(dialog).getByText("My database")).toBeVisible();
    expect(within(dialog).getByText("1.00 KiB")).toBeVisible();
    expect(apiPost).not.toHaveBeenCalled();
  });

  it("shows records and prevents selecting every recovery version", async () => {
    const user = userEvent.setup();
    render(<BackupHarness />);
    await screen.findByText("My backups");
    await user.click(screen.getByRole("button", { name: "Backups" }));
    await screen.findByText("new.aipdb");
    await waitFor(() => expect(screen.getByRole("button", { name: "Save policy" })).toBeEnabled());
    await user.click(screen.getByLabelText("Select old.aipdb"));
    expect(screen.getByLabelText("Select new.aipdb")).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "Delete selected (1)" }));
    const dialog = screen.getByRole("dialog", { name: "Delete backup version" });
    expect(within(dialog).getByText("old.aipdb")).toBeVisible();
    expect(apiPost).not.toHaveBeenCalled();
  });

  it("validates optional provider and record presentation metadata", () => {
    expect(isBackupProvider(provider)).toBe(true);
    expect(isBackupProvider({ ...provider, has_secret: "true" })).toBe(false);
    expect(isBackupProvider({ ...provider, public: { base_url: 4 } })).toBe(false);
    expect(isBackupRecord(records[0])).toBe(true);
    expect(isBackupRecord({ id: 1, size_bytes: 2 ** 53 })).toBe(true);
    expect(isBackupRecord({ id: 1, filename: {} })).toBe(false);
    expect(isBackupRecord({ id: 1, size_bytes: -1 })).toBe(false);
  });
});
