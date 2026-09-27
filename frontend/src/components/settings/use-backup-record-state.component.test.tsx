import { act, renderHook } from "@testing-library/react";
import type { FormEvent } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiDownload, apiGet, apiPost } from "../../lib/api";
import { useAsyncAction } from "../../lib/use-async-action";
import { suggestedRestoreDatabaseName, useBackupRecordState } from "./use-backup-record-state";
import type { BackupProvider, BackupRecord } from "./backup-contracts";

vi.mock("../../lib/api", () => ({ apiDownload: vi.fn(), apiGet: vi.fn(), apiPost: vi.fn() }));

const provider: BackupProvider = { id: 7, name: "Remote" };
const records: BackupRecord[] = [
  { id: 3, filename: "latest.aipdb", database_name: "Team Database" },
  { id: 2, filename: "older.aipdb" },
  { id: 1, filename: "oldest.aipdb" },
];
const event = () =>
  ({
    nativeEvent: new Event("submit"),
    currentTarget: document.createElement("form"),
    target: document.createElement("form"),
    bubbles: true,
    cancelable: true,
    defaultPrevented: false,
    eventPhase: 2,
    isTrusted: false,
    preventDefault: vi.fn(),
    isDefaultPrevented: () => false,
    stopPropagation: vi.fn(),
    isPropagationStopped: () => false,
    persist: vi.fn(),
    timeStamp: 0,
    type: "submit",
  }) satisfies FormEvent;
const idle = { state: "idle", error: null, message: null };

function deferred() {
  let resolve!: (_value: unknown) => void;
  let reject!: (_reason: unknown) => void;
  const promise = new Promise<unknown>((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}

function setup() {
  return renderHook(() => {
    const { actionState, runAction, resetAction } = useAsyncAction();
    return {
      ...useBackupRecordState({
        backupProviderState: actionState,
        runBackupProviderAction: runAction,
        resetBackupProviderAction: resetAction,
      }),
      actionState,
    };
  });
}

beforeEach(() => {
  vi.resetAllMocks();
  vi.mocked(apiGet).mockResolvedValue({ items: records });
  vi.mocked(apiPost).mockResolvedValue({ deleted_count: 1, keep_latest: 2 });
  vi.mocked(apiDownload).mockResolvedValue({ saved: true, method: "picker" });
});

describe("backup record browsing", () => {
  it("loads, refreshes, and resets selection and child dialogs on close", async () => {
    const { result } = setup();
    expect(result.current.backupRecords).toEqual({ state: "idle", data: [], error: null });
    await act(async () => result.current.refreshBackupRecords());
    expect(apiGet).not.toHaveBeenCalled();
    const loading = deferred();
    vi.mocked(apiGet).mockReturnValueOnce(loading.promise);
    let opening: Promise<void> | undefined;
    act(() => {
      opening = result.current.openBackupRecordsDialog(provider);
    });
    expect(result.current.backupRecordsProvider).toEqual(provider);
    expect(result.current.backupRecords).toEqual({ state: "loading", data: [], error: null });
    await act(async () => {
      loading.resolve({ items: records });
      await opening;
    });
    expect(result.current.backupRecords).toEqual({ state: "ready", data: records, error: null });
    act(() => result.current.toggleBackupRecordSelection(2));
    vi.mocked(apiGet).mockResolvedValueOnce({ items: [records[0], records[2]] });
    await act(async () => result.current.refreshBackupRecords());
    expect(apiGet).toHaveBeenLastCalledWith("/api/backup/providers/7/records");
    expect(result.current.backupRecords.data).toEqual([records[0], records[2]]);
    expect(result.current.selectedBackupRecordIDs).toEqual([]);
    act(() => {
      result.current.requestPruneBackupRecords();
      result.current.requestDeleteBackupRecords([records[2]]);
      result.current.toggleBackupRecordSelection(1);
    });
    act(() => result.current.closeBackupRecordsDialog());
    expect(result.current.backupRecordsProvider).toBeNull();
    expect(result.current.backupPruneTarget).toBeNull();
    expect(result.current.backupDeleteRecords).toEqual([]);
    expect(result.current.selectedBackupRecordIDs).toEqual([]);
    expect(result.current.backupRecords).toEqual({ state: "idle", data: [], error: null });
  });

  it("limits selection to preserve one backup, toggles off, and selects at most 100 older records", async () => {
    const { result } = setup();
    act(() => result.current.toggleBackupRecordSelection(1));
    expect(result.current.selectedBackupRecordIDs).toEqual([]);
    await act(async () => result.current.openBackupRecordsDialog(provider));
    act(() => {
      result.current.toggleBackupRecordSelection(2);
      result.current.toggleBackupRecordSelection(1);
      result.current.toggleBackupRecordSelection(3);
    });
    expect(result.current.selectedBackupRecordIDs).toEqual([2, 1]);
    expect(result.current.selectedBackupRecords).toEqual([records[1], records[2]]);
    act(() => result.current.toggleBackupRecordSelection(2));
    expect(result.current.selectedBackupRecordIDs).toEqual([1]);
    act(() => result.current.selectOlderBackupRecords());
    expect(result.current.selectedBackupRecordIDs).toEqual([2, 1]);
    act(() => result.current.requestDeleteBackupRecords([]));
    expect(result.current.backupDeleteRecords).toEqual([]);
    act(() => result.current.requestDeleteBackupRecords(records));
    expect(result.current.backupDeleteRecords).toEqual([]);
    vi.mocked(apiGet).mockResolvedValueOnce({ items: Array.from({ length: 120 }, (_, index) => ({ id: 120 - index })) });
    await act(async () => result.current.refreshBackupRecords());
    act(() => result.current.selectOlderBackupRecords());
    expect(result.current.selectedBackupRecordIDs).toEqual(Array.from({ length: 100 }, (_, index) => 119 - index));
    expect(result.current.selectedBackupRecordIDs).not.toContain(120);
  });

  it.each([new Error("Records offline"), { items: [{ id: "broken" }] }, { items: null }])(
    "reports transport or malformed-list failures (%s)",
    async (response) => {
      if (response instanceof Error) vi.mocked(apiGet).mockRejectedValueOnce(response);
      else vi.mocked(apiGet).mockResolvedValueOnce(response);
      const { result } = setup();
      await act(async () => result.current.openBackupRecordsDialog(provider));
      expect(result.current.backupRecords.state).toBe("error");
      expect(result.current.backupRecords.data).toEqual([]);
      expect(result.current.backupRecords.error).toMatch(response instanceof Error ? /Records offline/ : /Invalid backup items/);
      await act(async () => result.current.refreshBackupRecords());
      expect(result.current.backupRecords).toEqual({ state: "ready", data: records, error: null });
    },
  );

  it.each(["resolve", "reject"] as const)("ignores stale %s responses after provider switch or close", async (settle) => {
    const old = deferred();
    vi.mocked(apiGet).mockReturnValueOnce(old.promise);
    const { result } = setup();
    let opening: Promise<void> | undefined;
    act(() => {
      opening = result.current.openBackupRecordsDialog({ id: 1, name: "Old" });
    });
    await act(async () => result.current.openBackupRecordsDialog(provider));
    await act(async () => {
      old[settle](settle === "resolve" ? { items: [{ id: 99 }] } : new Error("Stale failure"));
      await opening;
    });
    expect(result.current.backupRecordsProvider).toEqual(provider);
    expect(result.current.backupRecords).toEqual({ state: "ready", data: records, error: null });
    const closing = deferred();
    vi.mocked(apiGet).mockReturnValueOnce(closing.promise);
    act(() => {
      opening = result.current.refreshBackupRecords();
    });
    act(() => result.current.closeBackupRecordsDialog());
    await act(async () => {
      closing[settle](settle === "resolve" ? { items: records } : new Error("Closed failure"));
      await opening;
    });
    expect(result.current.backupRecordsProvider).toBeNull();
    expect(result.current.backupRecords).toEqual({ state: "idle", data: [], error: null });
  });
});

describe("backup record actions", () => {
  it("does not mutate without an active provider or target", async () => {
    const { result } = setup();
    act(() => result.current.requestPruneBackupRecords());
    expect(result.current.backupPruneTarget).toBeNull();
    await act(async () => {
      await result.current.deleteBackupRecords(event());
      await result.current.pruneBackupRecords(event());
      await result.current.restoreBackupRecord(event());
      await result.current.downloadBackupRecord(records[0]);
    });
    expect(apiPost).not.toHaveBeenCalled();
    expect(apiDownload).not.toHaveBeenCalled();
    act(() => result.current.requestRestoreBackupRecord(records[0]));
    await act(async () => result.current.restoreBackupRecord(event()));
    expect(apiPost).not.toHaveBeenCalled();
    act(() => result.current.closeRestoreBackupRecordDialog());
    expect(result.current.restoreRecordTarget).toBeNull();
    expect(result.current.restoreRecordForm).toEqual({ database_name: "", database_password: "" });
    await act(async () => result.current.openBackupRecordsDialog(provider));
    await act(async () => {
      await result.current.deleteBackupRecords(event());
      await result.current.restoreBackupRecord(event());
    });
    expect(apiPost).not.toHaveBeenCalled();
  });

  it.each([1, 2])("deletes %s selected records, guards pending dialogs, retains failures for retry", async (count) => {
    const { result } = setup();
    await act(async () => result.current.openBackupRecordsDialog(provider));
    act(() => result.current.selectOlderBackupRecords());
    const deleting = records.slice(1, 1 + count);
    act(() => result.current.requestDeleteBackupRecords(deleting));
    act(() => result.current.closeDeleteBackupRecordsDialog());
    expect(result.current.backupDeleteRecords).toEqual([]);
    act(() => result.current.requestDeleteBackupRecords(deleting));
    const pending = deferred();
    vi.mocked(apiPost).mockReturnValueOnce(pending.promise);
    let submitting: Promise<void> | undefined;
    act(() => {
      submitting = result.current.deleteBackupRecords(event());
    });
    expect(result.current.actionState.state).toBe("deleting-records");
    act(() => {
      result.current.closeDeleteBackupRecordsDialog();
      result.current.closeBackupRecordsDialog();
    });
    expect(result.current.backupDeleteRecords).toEqual(deleting);
    expect(result.current.backupRecordsProvider).toEqual(provider);
    await act(async () => {
      pending.reject(new Error("Delete failed"));
      await submitting;
    });
    expect(result.current.actionState.error).toBe("Delete failed");
    expect(result.current.backupDeleteRecords).toEqual(deleting);
    expect(result.current.selectedBackupRecordIDs).toEqual([2, 1]);
    expect(apiGet).toHaveBeenCalledTimes(1);
    vi.mocked(apiPost).mockResolvedValueOnce({ deleted_count: count });
    vi.mocked(apiGet).mockResolvedValueOnce({ items: [records[0]] });
    await act(async () => result.current.deleteBackupRecords(event()));
    expect(apiPost).toHaveBeenLastCalledWith("/api/backup/providers/7/records/delete", { record_ids: deleting.map((record) => record.id) });
    expect(result.current.backupDeleteRecords).toEqual([]);
    expect(result.current.selectedBackupRecordIDs).toEqual([]);
    expect(result.current.backupRecords.data).toEqual([records[0]]);
    expect(result.current.actionState).toEqual(idle);
  });

  it.each([0, 1, 2])("prunes with validated retention and refreshes after deleting %s records", async (count) => {
    const { result } = setup();
    await act(async () => result.current.openBackupRecordsDialog(provider));
    act(() => result.current.requestPruneBackupRecords());
    expect(result.current.backupPruneKeepLatest).toBe("10");
    act(() => result.current.closePruneBackupRecordsDialog());
    expect(result.current.backupPruneTarget).toBeNull();
    act(() => result.current.requestPruneBackupRecords());
    act(() => result.current.setBackupPruneKeepLatest("0"));
    expect(result.current.parsedBackupPruneKeepLatest).toBeNull();
    await act(async () => result.current.pruneBackupRecords(event()));
    expect(apiPost).not.toHaveBeenCalled();
    act(() => result.current.setBackupPruneKeepLatest(" 2 "));
    expect(result.current.parsedBackupPruneKeepLatest).toBe(2);
    const pending = deferred();
    vi.mocked(apiPost).mockReturnValueOnce(pending.promise);
    let submitting: Promise<void> | undefined;
    act(() => {
      submitting = result.current.pruneBackupRecords(event());
    });
    expect(result.current.actionState.state).toBe("pruning");
    act(() => {
      result.current.closePruneBackupRecordsDialog();
      result.current.closeBackupRecordsDialog();
    });
    expect(result.current.backupPruneTarget).toEqual(provider);
    expect(result.current.backupRecordsProvider).toEqual(provider);
    await act(async () => {
      pending.reject(new Error("Prune failed"));
      await submitting;
    });
    expect(result.current.actionState.error).toBe("Prune failed");
    expect(result.current.backupPruneTarget).toEqual(provider);
    expect(result.current.backupPruneKeepLatest).toBe(" 2 ");
    expect(apiGet).toHaveBeenCalledTimes(1);
    vi.mocked(apiPost).mockResolvedValueOnce({ deleted_count: count, keep_latest: 2 });
    await act(async () => result.current.pruneBackupRecords(event()));
    expect(apiPost).toHaveBeenLastCalledWith("/api/backup/providers/7/prune", { keep_latest: 2 });
    expect(result.current.backupPruneTarget).toBeNull();
    expect(apiGet).toHaveBeenCalledTimes(2);
    expect(result.current.backupRecords.state).toBe("ready");
    expect(result.current.actionState).toEqual(idle);
  });

  it("downloads with streaming, supports cancellation and filename fallback, and reports failure", async () => {
    const { result } = setup();
    await act(async () => result.current.openBackupRecordsDialog(provider));
    await act(async () => result.current.downloadBackupRecord(records[0]));
    expect(apiDownload).toHaveBeenLastCalledWith("/api/backup/providers/7/records/3/download", "latest.aipdb", {
      picker: true,
      requireStreaming: true,
    });
    expect(result.current.actionState.message).toBe("Downloaded latest.aipdb.");
    vi.mocked(apiDownload).mockResolvedValueOnce({ saved: false, canceled: true, method: "picker" });
    await act(async () => result.current.downloadBackupRecord({ id: 1 }));
    expect(apiDownload).toHaveBeenLastCalledWith("/api/backup/providers/7/records/1/download", "aipermission-backup.aipdb", {
      picker: true,
      requireStreaming: true,
    });
    expect(result.current.actionState).toEqual(idle);
    vi.mocked(apiDownload).mockRejectedValueOnce(new Error("Stream interrupted"));
    await act(async () => result.current.downloadBackupRecord(records[0]));
    expect(result.current.actionState).toEqual({ state: "error", error: "Stream interrupted", message: null });
    act(() => result.current.requestRestoreBackupRecord(records[0]));
    expect(result.current.actionState).toEqual(idle);
  });

  it("restores with credentials, blocks closing while pending, retries failures, and schedules reload", async () => {
    const { result } = setup();
    await act(async () => result.current.openBackupRecordsDialog(provider));
    act(() => result.current.requestRestoreBackupRecord(records[0]));
    expect(result.current.restoreRecordForm).toEqual({ database_name: "Team-Database-restore", database_password: "" });
    act(() => result.current.setRestoreRecordForm({ database_name: "Recovered", database_password: "restore-secret" }));
    const pending = deferred();
    vi.mocked(apiPost).mockReturnValueOnce(pending.promise);
    let submitting: Promise<void> | undefined;
    act(() => {
      submitting = result.current.restoreBackupRecord(event());
    });
    expect(result.current.actionState.state).toBe("restoring-3");
    act(() => {
      result.current.closeRestoreBackupRecordDialog();
      result.current.closeBackupRecordsDialog();
    });
    expect(result.current.restoreRecordTarget).toEqual(records[0]);
    expect(result.current.backupRecordsProvider).toEqual(provider);
    await act(async () => {
      pending.reject(new Error("Wrong password"));
      await submitting;
    });
    expect(result.current.actionState.error).toBe("Wrong password");
    expect(result.current.restoreRecordForm.database_password).toBe("restore-secret");
    expect(result.current.restoreRecordTarget).toEqual(records[0]);
    vi.useFakeTimers();
    const timeout = vi.spyOn(window, "setTimeout");
    await act(async () => result.current.restoreBackupRecord(event()));
    expect(apiPost).toHaveBeenLastCalledWith("/api/backup/providers/7/records/3/restore", {
      database_name: "Recovered",
      database_password: "restore-secret",
    });
    expect(result.current.actionState.message).toBe("Restored latest.aipdb as Recovered.");
    expect(result.current.restoreRecordTarget).toBeNull();
    expect(result.current.restoreRecordForm).toEqual({ database_name: "", database_password: "" });
    expect(timeout).toHaveBeenCalledWith(expect.any(Function), 800);
    // Execute the scheduled callback against a stub, never the real browser location.
    const reload = vi.fn();
    const callback = timeout.mock.calls.find(([, delay]) => delay === 800)?.[0];
    if (typeof callback !== "function") throw new Error("Expected the scheduled reload callback.");
    vi.stubGlobal("window", { location: { reload } });
    callback();
    expect(reload).toHaveBeenCalledOnce();
    vi.unstubAllGlobals();
    timeout.mockRestore();
    vi.useRealTimers();
    expect(apiGet).toHaveBeenCalledTimes(1);
  });

  it.each([
    [{ id: 3, database_name: "  Team / Data!  " }, "Team-Data-restore"],
    [{ id: 3, database_id: "db_123" }, "db_123-restore"],
    [{ id: 3, database_name: "***" }, "restored-backup-restore"],
    [null, "restored-backup-restore"],
  ] satisfies [BackupRecord | null, string][])("suggests a sanitized restore name for %s", (record, name) => {
    expect(suggestedRestoreDatabaseName(record)).toBe(name);
  });
});
