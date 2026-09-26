import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiGet } from "../lib/api";
import { SettingsPage } from "./settings";
import { settingsDatabaseResponse } from "../lib/gateway-contracts/settings-database-contract";

vi.mock("../lib/api", () => ({ apiGet: vi.fn() }));
vi.mock("../components/settings/use-backup-provider-state", () => ({ useBackupProviderState: () => ({}) }));
vi.mock("../components/settings/backup-provider-dialogs", () => ({ BackupProviderDialogs: () => null }));
vi.mock("../components/settings/backup-provider-panel", () => ({ BackupProviderPanel: () => null }));
vi.mock("../components/settings/backup-record-dialogs", () => ({ BackupRecordDialogs: () => null }));
vi.mock("../components/settings/database-settings-panel", () => ({ DatabaseSettingsPanel: ({ databaseName }: { databaseName: string }) => <span>{databaseName}</span> }));
vi.mock("../components/settings/diagnostics-panel", () => ({ DiagnosticsPanel: () => null }));
vi.mock("../components/settings/history-labels-panel", () => ({ HistoryLabelsPanel: () => null }));
vi.mock("../components/settings/history-retention-panel", () => ({ HistoryRetentionPanel: () => null }));
vi.mock("../components/settings/maintenance-console-panel", () => ({ MaintenanceConsolePanel: () => null }));
vi.mock("../components/settings/local-action-retry-panel", () => ({ LocalActionRetryPanel: () => null }));

describe("settings database status ownership", () => {
  beforeEach(() => { vi.mocked(apiGet).mockReset(); });

  it("uses the current database name after a valid status read", async () => {
    vi.mocked(apiGet).mockResolvedValue({ database_name: "My database", database_size_bytes: 1024 });
    render(<SettingsPage />);
    expect(await screen.findByText("My database")).toBeVisible();
  });

  it("reports a malformed status without passing it to lifecycle forms", async () => {
    vi.mocked(apiGet).mockResolvedValue({ database_name: {} });
    render(<SettingsPage />);
    expect(await screen.findByText("Database name response is invalid.")).toBeVisible();
    expect(screen.getByText("Unknown")).toBeVisible();
  });

  it("invalidates the status read when the settings page unmounts", async () => {
    vi.mocked(apiGet).mockReturnValue(new Promise(() => {}));
    const { unmount } = render(<SettingsPage />);
    await waitFor(() => expect(apiGet).toHaveBeenCalledOnce());
    const options: unknown = vi.mocked(apiGet).mock.calls[0][1];
    if (!options || typeof options !== "object" || !("signal" in options) || !(options.signal instanceof AbortSignal)) throw new Error("Expected an owned database status read.");
    unmount();
    expect(options.signal.aborted).toBe(true);
  });

  it.each([null, [], { database_size_bytes: -1 }, { database_size_bytes: "1024" }, { database_size_bytes: Infinity }])("rejects invalid database presentation data %j", (value) => {
    expect(() => settingsDatabaseResponse(value)).toThrow();
  });

  it("accepts omitted locked database metadata and large backend byte totals", () => {
    expect(settingsDatabaseResponse({ state: "locked" })).toEqual({ state: "locked" });
    expect(settingsDatabaseResponse({ database_size_bytes: 2 ** 53 }).database_size_bytes).toBe(2 ** 53);
  });
});
