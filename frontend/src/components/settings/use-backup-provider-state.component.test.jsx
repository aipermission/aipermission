import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiGet, apiPost } from "../../lib/api";
import { useBackupProviderState } from "./use-backup-provider-state";

vi.mock("../../lib/api", () => ({
  apiDelete: vi.fn(),
  apiDownload: vi.fn(),
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

function renderBackupState() {
  return renderHook(() =>
    useBackupProviderState({ state: "ready", data: { database_name: "Default", database_size_bytes: 1024 }, error: null }),
  );
}

describe("useBackupProviderState", () => {
  beforeEach(() => {
    apiGet.mockImplementation(async (path) => {
      if (path === "/api/backup/providers/catalog")
        return { items: [{ provider_type: "aipermission_backup", label: "AIPermission Backup" }] };
      if (path === "/api/backup/providers") return { items: [] };
      throw new Error(`Unexpected GET ${path}`);
    });
  });

  it("clears provider tokens when the editor closes or saves", async () => {
    apiPost.mockResolvedValue({ id: 4 });
    const { result } = renderBackupState();
    await waitFor(() => expect(result.current.backupProviderCatalog.state).toBe("ready"));

    act(() => result.current.openBackupProviderDialog());
    act(() => result.current.updateBackupProviderField("token", "secret-token"));
    act(() => result.current.closeBackupProviderDialog());
    expect(result.current.backupProviderDialogOpen).toBe(false);
    expect(result.current.backupProviderForm.token).toBe("");

    act(() => result.current.openBackupProviderDialog());
    act(() => {
      result.current.updateBackupProviderField("name", "Private backup");
      result.current.updateBackupProviderField("base_url", "https://backup.example.com");
      result.current.updateBackupProviderField("token", "another-secret-token");
    });
    await act(async () => result.current.saveBackupProvider({ preventDefault() {} }));

    expect(apiPost).toHaveBeenCalledWith("/api/backup/providers", {
      provider_type: "aipermission_backup",
      name: "Private backup",
      public: { base_url: "https://backup.example.com" },
      secret: { token: "another-secret-token" },
    });
    expect(result.current.backupProviderDialogOpen).toBe(false);
    expect(result.current.backupProviderForm.token).toBe("");
  });

  it("ignores backup records returned for an older provider selection", async () => {
    const older = deferred();
    const newer = deferred();
    apiGet.mockImplementation((path) => {
      if (path === "/api/backup/providers/catalog" || path === "/api/backup/providers") return Promise.resolve({ items: [] });
      if (path === "/api/backup/providers/1/records") return older.promise;
      if (path === "/api/backup/providers/2/records") return newer.promise;
      return Promise.reject(new Error(`Unexpected GET ${path}`));
    });
    const { result } = renderBackupState();

    act(() => void result.current.openBackupRecordsDialog({ id: 1, name: "Older" }));
    act(() => void result.current.openBackupRecordsDialog({ id: 2, name: "Newer" }));
    await act(async () => newer.resolve({ items: [{ id: "newer-record" }] }));
    await waitFor(() => expect(result.current.backupRecords.data).toEqual([{ id: "newer-record" }]));
    await act(async () => older.resolve({ items: [{ id: "stale-record" }] }));

    expect(result.current.backupRecordsProvider.id).toBe(2);
    expect(result.current.backupRecords.data).toEqual([{ id: "newer-record" }]);
  });
});
