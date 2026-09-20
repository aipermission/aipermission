import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiDownload, apiPostForm, currentWorkspaceBinding } from "../../../lib/api";
import { APIError } from "../../../lib/errors";
import {
  completeLocalActionRetry,
  markLocalActionRetryOutcome,
  prepareLocalActionRetry,
  preserveLocalActionRetryAttempt,
} from "../../../lib/local-action-retry";
import { usePostgresBackupRestore } from "./use-postgres-backup-restore";

vi.mock("../../../lib/api", () => ({ apiDownload: vi.fn(), apiPostForm: vi.fn(), currentWorkspaceBinding: vi.fn() }));
vi.mock("../../../lib/local-action-retry", () => ({
  prepareLocalActionRetry: vi.fn(),
  completeLocalActionRetry: vi.fn(),
  markLocalActionRetryOutcome: vi.fn(),
  preserveLocalActionRetryAttempt: vi.fn(),
}));

beforeEach(() => {
  apiDownload.mockReset().mockResolvedValue({ saved: true });
  apiPostForm.mockReset().mockResolvedValue({ operation_id: 1, status: "completed", result: {} });
  currentWorkspaceBinding.mockReset().mockReturnValue("workspace-a");
  prepareLocalActionRetry.mockReset().mockResolvedValue({ idempotencyKey: "restore-attempt-key" });
  completeLocalActionRetry.mockReset().mockResolvedValue(undefined);
  markLocalActionRetryOutcome.mockReset().mockResolvedValue(undefined);
  preserveLocalActionRetryAttempt.mockReset().mockResolvedValue(undefined);
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
    expect.objectContaining({ picker: true, requireStreaming: true, signal: expect.any(AbortSignal) }),
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
    expect.objectContaining({ signal: expect.any(AbortSignal), workspaceBinding: "workspace-a" }),
  );
  expect(prepareLocalActionRetry).toHaveBeenCalledWith(expect.any(Object), { workspaceID: "workspace-a" });
  const submitted = apiPostForm.mock.calls[0][1];
  expect(submitted.get("dump")).toBe(dump);
  expect(submitted.get("confirm_target")).toBe("Main DB 1");
  expect(submitted.get("idempotency_key")).toBe("restore-attempt-key");
  expect(result.current.restoreState).toEqual({ state: "ready", error: "", message: "Restore completed." });
  expect(result.current.file).toBeNull();
  expect(completeLocalActionRetry).toHaveBeenCalledTimes(1);
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
  await waitFor(() => expect(resolveRestore).toBeTypeOf("function"));

  rerender(operation(2));
  await act(async () => {
    resolveRestore({ operation_id: 1, status: "completed", result: {} });
    await restorePromise;
  });

  expect(result.current.restoreState).toEqual({ state: "idle", error: "", message: "" });
  expect(result.current.file).toBeNull();
  expect(result.current.confirmTarget).toBe("");
});

it("reuses one restore identity after an uncertain client failure", async () => {
  apiPostForm.mockRejectedValueOnce(new Error("response lost")).mockResolvedValueOnce({
    operation_id: 1,
    status: "completed",
    replayed: true,
    result: {},
  });
  const { result } = renderHook(() => usePostgresBackupRestore(operation()));
  const dump = new File(["SELECT 1;"], "backup.sql", { type: "text/plain", lastModified: 7 });
  act(() => {
    result.current.setFile(dump);
    result.current.setConfirmTarget("Main DB 1");
  });

  await act(async () => result.current.restoreBackup({ preventDefault: vi.fn() }));
  await act(async () => result.current.restoreBackup({ preventDefault: vi.fn() }));

  expect(apiPostForm).toHaveBeenCalledTimes(2);
  expect(apiPostForm.mock.calls[1][1].get("idempotency_key")).toBe(apiPostForm.mock.calls[0][1].get("idempotency_key"));
  expect(preserveLocalActionRetryAttempt).toHaveBeenCalledTimes(1);
  expect(completeLocalActionRetry).toHaveBeenCalledTimes(1);
  expect(result.current.restoreState).toEqual({ state: "ready", error: "", message: "Restore completed." });
  expect(result.current.file).toBeNull();
  expect(result.current.confirmTarget).toBe("");
});

it("records an explicitly uncertain restore without preserving it as a normal retry", async () => {
  const response = { status: "outcome_unknown", operation_id: 7, code: "transport_lost" };
  apiPostForm.mockRejectedValueOnce(new APIError("outcome unknown", { status: 409, code: "transport_lost", data: response }));
  const { result } = renderHook(() => usePostgresBackupRestore(operation()));
  act(() => {
    result.current.setFile(new File(["SELECT 1;"], "backup.sql", { type: "text/plain" }));
    result.current.setConfirmTarget("Main DB 1");
  });

  await act(async () => result.current.restoreBackup({ preventDefault: vi.fn() }));

  expect(markLocalActionRetryOutcome).toHaveBeenCalledWith(expect.any(Object), response);
  expect(preserveLocalActionRetryAttempt).not.toHaveBeenCalled();
});

it("leaves the running state when retry-ledger settlement fails", async () => {
  apiPostForm.mockRejectedValueOnce(new Error("response lost"));
  preserveLocalActionRetryAttempt.mockRejectedValueOnce(new Error("ledger unavailable"));
  const { result } = renderHook(() => usePostgresBackupRestore(operation()));
  act(() => {
    result.current.setFile(new File(["SELECT 1;"], "backup.sql", { type: "text/plain" }));
    result.current.setConfirmTarget("Main DB 1");
  });

  await act(async () => result.current.restoreBackup({ preventDefault: vi.fn() }));

  expect(result.current.restoreState).toMatchObject({
    state: "error",
    error: expect.stringContaining("Inspect the database before retrying"),
  });
  expect(result.current.isWorking).toBe(false);
});

it("preserves the retry identity when a successful response lacks the restore completion envelope", async () => {
  apiPostForm.mockResolvedValueOnce({ ok: true });
  const { result } = renderHook(() => usePostgresBackupRestore(operation()));
  act(() => {
    result.current.setFile(new File(["SELECT 1;"], "backup.sql", { type: "text/plain" }));
    result.current.setConfirmTarget("Main DB 1");
  });

  await act(async () => result.current.restoreBackup({ preventDefault: vi.fn() }));

  expect(markLocalActionRetryOutcome).toHaveBeenCalledWith(expect.any(Object), { status: "outcome_unknown" });
  expect(completeLocalActionRetry).not.toHaveBeenCalled();
  expect(result.current.restoreState).toMatchObject({ state: "error", error: "Invalid restore completion response from gateway." });
});

it("releases a definitive artifact conflict so the corrected file can use a new identity", async () => {
  apiPostForm.mockRejectedValueOnce(new APIError("different artifact", { status: 409, code: "idempotency_conflict" }));
  const { result } = renderHook(() => usePostgresBackupRestore(operation()));
  act(() => {
    result.current.setFile(new File(["SELECT 2;"], "backup.sql", { type: "text/plain" }));
    result.current.setConfirmTarget("Main DB 1");
  });

  await act(async () => result.current.restoreBackup({ preventDefault: vi.fn() }));

  expect(completeLocalActionRetry).toHaveBeenCalledTimes(1);
  expect(preserveLocalActionRetryAttempt).not.toHaveBeenCalled();
});
