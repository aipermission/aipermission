import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiDelete, apiDownload, apiGet, apiPost, apiPut } from "../../lib/api";
import { backupProviderLabel, useBackupProviderState } from "./use-backup-provider-state";

vi.mock("../../lib/api", () => ({
  apiDelete: vi.fn(),
  apiDownload: vi.fn(),
  apiGet: vi.fn(),
  apiPost: vi.fn(),
  apiPut: vi.fn(),
}));

const provider = { id: 7, name: "Remote", provider_type: "custom", public: { base_url: "https://backup.example" } };
const event = () => ({ preventDefault: vi.fn() });
const idle = { state: "idle", error: null, message: null };

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}

async function setup(database = { data: { database_name: "Default" } }) {
  const hook = renderHook(() => useBackupProviderState(database));
  await waitFor(() => expect(hook.result.current.backupProviders.state).toBe("ready"));
  await waitFor(() => expect(hook.result.current.backupProviderCatalog.state).toBe("ready"));
  return hook;
}

describe("backup provider behavior", () => {
  beforeEach(() => {
    vi.resetAllMocks();
    apiGet.mockImplementation(async (path) => {
      if (path.endsWith("/catalog")) return { items: [{ provider_type: "custom", label: "Custom Backup" }] };
      if (path === "/api/backup/providers") return { items: [provider] };
      throw new Error(`Unexpected GET ${path}`);
    });
    apiPost.mockResolvedValue({ filename: "snapshot.aipdb" });
    apiPut.mockResolvedValue({});
    apiDelete.mockResolvedValue({});
  });

  it("uses catalog defaults, resets the editor, and trims new connection secrets", async () => {
    const { result } = await setup();
    expect(result.current.backupProviders.data).toEqual([provider]);
    act(() => result.current.openBackupProviderDialog());
    expect(result.current.backupProviderForm).toEqual({ provider_type: "custom", name: "Custom Backup", base_url: "", token: "" });
    act(() => {
      result.current.updateBackupProviderField("base_url", "  https://new.example  ");
      result.current.updateBackupProviderField("token", "  secret  ");
    });
    const submit = event();
    await act(async () => result.current.saveBackupProvider(submit));
    expect(submit.preventDefault).toHaveBeenCalledOnce();
    expect(apiPost).toHaveBeenCalledWith("/api/backup/providers", {
      provider_type: "custom",
      name: "Custom Backup",
      public: { base_url: "https://new.example" },
      secret: { token: "secret" },
    });
    expect(result.current.backupProviderState.message).toBe("Backup provider added.");
    expect(result.current.backupProviderDialogOpen).toBe(false);
    expect(result.current.backupProviderEditingID).toBeNull();
    expect(result.current.backupProviderForm.token).toBe("");
    expect(apiGet.mock.calls.filter(([path]) => path === "/api/backup/providers")).toHaveLength(2);
    act(() => result.current.openBackupProviderDialog());
    expect(result.current.backupProviderState).toEqual(idle);
    act(() => result.current.closeBackupProviderDialog());
    expect(result.current.backupProviderDialogOpen).toBe(false);
  });

  it("edits without replacing a stored token and cannot close during save", async () => {
    const pending = deferred();
    apiPut.mockReturnValueOnce(pending.promise);
    const { result } = await setup();
    act(() => result.current.openBackupProviderDialog(provider));
    expect(result.current.backupProviderEditingID).toBe(7);
    expect(result.current.backupProviderForm).toEqual({
      provider_type: "custom",
      name: "Remote",
      base_url: "https://backup.example",
      token: "",
    });
    act(() => result.current.updateBackupProviderField("token", "   "));
    let saving;
    act(() => {
      saving = result.current.saveBackupProvider(event());
    });
    expect(result.current.backupProviderState.state).toBe("saving");
    act(() => result.current.closeBackupProviderDialog());
    expect(result.current.backupProviderDialogOpen).toBe(true);
    expect(apiPut).toHaveBeenCalledWith("/api/backup/providers/7", {
      provider_type: "custom",
      name: "Remote",
      public: { base_url: "https://backup.example" },
    });
    await act(async () => {
      pending.resolve({});
      await saving;
    });
    expect(result.current.backupProviderState.message).toBe("Backup provider updated.");
    expect(result.current.backupProviderDialogOpen).toBe(false);
  });

  it("retains failed connection input for retry and supports catalog/editor fallbacks", async () => {
    apiGet.mockResolvedValue({ items: [] });
    apiPost.mockRejectedValueOnce(new Error("Connection refused"));
    const { result } = await setup();
    act(() => result.current.openBackupProviderDialog());
    expect(result.current.backupProviderForm.name).toBe("aipermission_backup");
    act(() => result.current.updateBackupProviderField("token", "retry-secret"));
    await act(async () => result.current.saveBackupProvider(event()));
    expect(result.current.backupProviderState).toEqual({ state: "error", error: "Connection refused", message: null });
    expect(result.current.backupProviderDialogOpen).toBe(true);
    expect(result.current.backupProviderForm.token).toBe("retry-secret");
    act(() => result.current.openBackupProviderDialog({ id: 9, name: "Legacy" }));
    expect(result.current.backupProviderState).toEqual(idle);
    expect(result.current.backupProviderForm).toEqual({ provider_type: "aipermission_backup", name: "Legacy", base_url: "", token: "" });
    expect(backupProviderLabel("missing", [])).toBe("missing");
  });

  it("surfaces catalog and provider transport failures independently", async () => {
    apiGet.mockImplementation(async (path) => {
      throw new Error(path.endsWith("catalog") ? "Catalog offline" : "Providers offline");
    });
    const { result } = renderHook(() => useBackupProviderState({}));
    await waitFor(() => expect(result.current.backupProviders.state).toBe("error"));
    expect(result.current.backupProviders).toEqual({ state: "error", data: [], error: "Providers offline" });
    expect(result.current.backupProviderCatalog).toEqual({ state: "error", data: [], error: "Catalog offline" });
  });

  it.each([false, true])("probes and disables providers, refreshing even after failure (%s)", async (fail) => {
    const { result } = await setup();
    if (fail) apiPost.mockRejectedValueOnce(new Error("Incompatible protocol"));
    await act(async () => result.current.testBackupProvider(provider));
    expect(apiPost).toHaveBeenCalledWith("/api/backup/providers/7/test", {});
    expect(result.current.backupProviderState).toEqual(
      fail
        ? { state: "error", error: "Incompatible protocol", message: null }
        : { state: "idle", error: null, message: "Remote is reachable and protocol-compatible." },
    );
    if (fail) apiPut.mockRejectedValueOnce(new Error("Disable refused"));
    await act(async () => result.current.disableBackupProvider(provider));
    expect(apiPut).toHaveBeenCalledWith("/api/backup/providers/7", { name: "Remote", status: "disabled" });
    expect(result.current.backupProviderState.error).toBe(fail ? "Disable refused" : null);
    expect(result.current.backupProviderState.message).toBe(fail ? null : "Remote disabled.");
    expect(apiGet.mock.calls.filter(([path]) => path === "/api/backup/providers")).toHaveLength(3);
  });

  it.each([
    {
      request: "requestEnableBackupProvider",
      close: "closeEnableBackupProviderDialog",
      submit: "enableBackupProvider",
      target: "backupEnableTarget",
      pending: "enabling-7",
      api: apiPost,
      path: "/api/backup/providers/7/enable",
      payload: { current_password: "password" },
      message: "Remote enabled.",
    },
    {
      request: "requestArchiveBackupProvider",
      close: "closeBackupProviderArchiveDialog",
      submit: "archiveBackupProvider",
      target: "backupProviderArchiveTarget",
      pending: "archiving",
      api: apiDelete,
      path: "/api/backup/providers/7",
      message: 'Archived backup provider "Remote".',
    },
    {
      request: "requestUploadBackupProvider",
      close: "closeUploadBackupDialog",
      submit: "uploadBackupProvider",
      target: "backupUploadTarget",
      pending: "uploading-7",
      api: apiPost,
      path: "/api/backup/providers/7/upload",
      payload: {},
      message: "Uploaded snapshot.aipdb to Remote.",
    },
  ])("guards $submit, retains failures, then refreshes on retry", async (operation) => {
    const { result } = await setup();
    await act(async () => result.current[operation.submit](event()));
    expect(operation.api).not.toHaveBeenCalled();
    act(() => result.current[operation.request](provider));
    expect(result.current.backupProviderState).toEqual(idle);
    act(() => result.current[operation.close]());
    expect(result.current[operation.target]).toBeNull();
    act(() => result.current[operation.request](provider));
    if (operation.submit === "enableBackupProvider") act(() => result.current.setBackupEnablePassword("password"));
    const pending = deferred();
    operation.api.mockReturnValueOnce(pending.promise);
    let submitting;
    act(() => {
      submitting = result.current[operation.submit](event());
    });
    expect(result.current.backupProviderState.state).toBe(operation.pending);
    act(() => result.current[operation.close]());
    expect(result.current[operation.target]).toEqual(provider);
    await act(async () => {
      pending.reject(new Error("Transport unavailable"));
      await submitting;
    });
    expect(result.current.backupProviderState.error).toBe("Transport unavailable");
    expect(result.current[operation.target]).toEqual(provider);
    expect(apiGet.mock.calls.filter(([path]) => path === "/api/backup/providers")).toHaveLength(1);
    await act(async () => result.current[operation.submit](event()));
    expect(operation.api).toHaveBeenLastCalledWith(...(operation.payload ? [operation.path, operation.payload] : [operation.path]));
    expect(result.current[operation.target]).toBeNull();
    expect(result.current.backupProviderState.message).toBe(operation.message);
    expect(apiGet.mock.calls.filter(([path]) => path === "/api/backup/providers")).toHaveLength(2);
    if (operation.submit === "enableBackupProvider") expect(result.current.backupEnablePassword).toBe("");
  });

  it("ignores a stale probe failure after opening a new action dialog", async () => {
    const pending = deferred();
    apiPost.mockReturnValueOnce(pending.promise);
    const { result } = await setup();
    let probing;
    act(() => {
      probing = result.current.testBackupProvider(provider);
    });
    expect(result.current.backupProviderState.state).toBe("testing-7");
    act(() => result.current.requestEnableBackupProvider(provider));
    await act(async () => {
      pending.reject(new Error("Stale probe"));
      await probing;
    });
    expect(result.current.backupProviderState).toEqual(idle);
    expect(result.current.backupEnableTarget).toEqual(provider);
  });

  it("uses the database-name fallback and reports streaming transport errors", async () => {
    apiDownload.mockRejectedValueOnce(new Error("Streaming required"));
    const { result } = await setup({});
    expect(result.current.databaseName).toBe("Unknown");
    await act(async () => result.current.downloadDatabase());
    expect(apiDownload).toHaveBeenCalledWith("/api/backup/download", expect.stringMatching(/^Unknown-.*\.aipdb$/), {
      picker: true,
      requireStreaming: true,
    });
    expect(result.current.backupState).toEqual({ state: "error", error: "Streaming required", message: null });
    expect(result.current.backupProviderState).toEqual(idle);
  });
});
