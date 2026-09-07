import { act, renderHook } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiDownload, apiPostForm } from "../../../lib/api";
import { usePostgresBackupRestore } from "./use-postgres-backup-restore";

vi.mock("../../../lib/api", () => ({ apiDownload: vi.fn(), apiPostForm: vi.fn() }));

beforeEach(() => {
  apiDownload.mockReset().mockResolvedValue({ saved: true });
  apiPostForm.mockReset().mockResolvedValue({ ok: true });
});

function operation(id = 1) {
  return {
    open: true,
    target: { id, name: `Main DB ${id}` },
    profile: { id: id * 10 },
  };
}

it("downloads a safe filename and keeps canceled pickers idle", async () => {
  apiDownload.mockResolvedValueOnce({ canceled: true });
  const { result } = renderHook(() => usePostgresBackupRestore(operation()));

  await act(async () => result.current.downloadBackup());

  expect(apiDownload).toHaveBeenCalledWith(
    "/api/connector-targets/1/profiles/10/backup",
    "main-db-1.sql",
    expect.objectContaining({ picker: true, signal: expect.any(AbortSignal) }),
  );
  expect(result.current.backupState).toEqual({ state: "idle", error: "", message: "" });
});

it("discards a backup completion after the selected profile changes", async () => {
  let resolveDownload;
  apiDownload.mockImplementationOnce(() => new Promise((resolve) => (resolveDownload = resolve)));
  const { result, rerender } = renderHook((value) => usePostgresBackupRestore(value), { initialProps: operation() });
  let downloadPromise;
  act(() => {
    downloadPromise = result.current.downloadBackup();
  });
  rerender(operation(2));

  await act(async () => {
    resolveDownload({ saved: true });
    await downloadPromise;
  });

  expect(result.current.backupState).toEqual({ state: "idle", error: "", message: "" });
});

it("restores only after exact target confirmation and captures the selected dump", async () => {
  const { result } = renderHook(() => usePostgresBackupRestore(operation()));
  const dump = new File(["SELECT 1;"], "backup.sql", { type: "text/plain" });
  act(() => {
    result.current.setActiveTab("restore");
    result.current.setFile(dump);
    result.current.setConfirmTarget("Main DB 1");
  });
  expect(result.current.restoreReady).toBe(true);

  await act(async () => result.current.restoreBackup({ preventDefault: vi.fn() }));

  expect(apiPostForm).toHaveBeenCalledWith(
    "/api/connector-targets/1/profiles/10/restore",
    expect.any(FormData),
    expect.objectContaining({ signal: expect.any(AbortSignal) }),
  );
  const submitted = apiPostForm.mock.calls[0][1];
  expect(submitted.get("dump")).toBe(dump);
  expect(submitted.get("confirm_target")).toBe("Main DB 1");
  expect(result.current.restoreState).toEqual({ state: "ready", error: "", message: "Restore completed." });
  expect(result.current.file).toBeNull();
});

it("does not submit a restore with an inexact target confirmation", async () => {
  const { result } = renderHook(() => usePostgresBackupRestore(operation()));
  act(() => {
    result.current.setFile(new File(["SELECT 1;"], "backup.sql", { type: "text/plain" }));
    result.current.setConfirmTarget("main db 1");
  });

  await act(async () => result.current.restoreBackup({ preventDefault: vi.fn() }));

  expect(result.current.restoreReady).toBe(false);
  expect(apiPostForm).not.toHaveBeenCalled();
});

it("does not apply a restore completion after the target profile changes", async () => {
  let resolveRestore;
  apiPostForm.mockImplementationOnce(() => new Promise((resolve) => (resolveRestore = resolve)));
  const { result, rerender } = renderHook((value) => usePostgresBackupRestore(value), { initialProps: operation() });
  act(() => {
    result.current.setFile(new File(["SELECT 1;"], "backup.sql", { type: "text/plain" }));
    result.current.setConfirmTarget("Main DB 1");
  });
  let restorePromise;
  act(() => {
    restorePromise = result.current.restoreBackup({ preventDefault: vi.fn() });
  });

  rerender(operation(2));
  await act(async () => {
    resolveRestore({ ok: true });
    await restorePromise;
  });

  expect(result.current.restoreState).toEqual({ state: "idle", error: "", message: "" });
  expect(result.current.file).toBeNull();
  expect(result.current.confirmTarget).toBe("");
});
