import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiDownload, apiPostForm, currentWorkspaceBinding } from "../../../lib/api";
import { APIError } from "../../../lib/errors";
import {
  completeLocalActionRetry,
  markLocalActionRetryOutcome,
  prepareLocalActionRetry,
  preserveLocalActionRetryAttempt,
  releaseLocalActionRetryAttempt,
} from "../../../lib/local-action-retry";
import { postgresRestoreRetryIdentity } from "./postgres-restore-identity";
import { requireCompletedRestoreResponse, settleRestoreRetryFailure, usePostgresBackupRestore } from "./use-postgres-backup-restore";
import type { PostgresOperation } from "./operation-types";

const download = vi.mocked(apiDownload);
const postForm = vi.mocked(apiPostForm);
const workspaceBinding = vi.mocked(currentWorkspaceBinding);
const prepareRetry = vi.mocked(prepareLocalActionRetry);
const completeRetry = vi.mocked(completeLocalActionRetry);
const markOutcome = vi.mocked(markLocalActionRetryOutcome);
const preserveAttempt = vi.mocked(preserveLocalActionRetryAttempt);

vi.mock("../../../lib/api", () => ({ apiDownload: vi.fn(), apiPostForm: vi.fn(), currentWorkspaceBinding: vi.fn() }));
vi.mock("../../../lib/local-action-retry", () => ({
  prepareLocalActionRetry: vi.fn(),
  completeLocalActionRetry: vi.fn(),
  markLocalActionRetryOutcome: vi.fn(),
  preserveLocalActionRetryAttempt: vi.fn(),
  releaseLocalActionRetryAttempt: vi.fn(),
}));

beforeEach(() => {
  download.mockReset().mockResolvedValue({ saved: true, method: "picker" });
  postForm.mockReset().mockResolvedValue({ operation_id: 1, status: "completed", result: {} });
  workspaceBinding.mockReset().mockReturnValue("workspace-a");
  prepareRetry.mockReset().mockResolvedValue({
    scope: { key: "workspace-a", legacyKey: "legacy-workspace-a" },
    signature: "test-signature",
    idempotencyKey: "restore-attempt-key",
    revision: 1,
    attemptID: "test-attempt",
    reused: false,
  });
  completeRetry.mockReset().mockResolvedValue(undefined);
  markOutcome.mockReset().mockResolvedValue(undefined);
  preserveAttempt.mockReset().mockResolvedValue(undefined);
  vi.mocked(releaseLocalActionRetryAttempt).mockReset().mockResolvedValue(undefined);
});

function operation(id = 1): PostgresOperation {
  return {
    open: true,
    target: { id, name: `Main DB ${id}` },
    profile: { id: id * 10 },
  };
}

it.each([false, true])("releases an undispatched restore after preparation is canceled (reused: %s)", async (reused) => {
  const retry = {
    scope: { key: "workspace-a", legacyKey: "legacy" },
    signature: "test",
    idempotencyKey: "test-key",
    revision: 1,
    attemptID: "test",
    reused,
  };
  let resolvePreparation: ((_value: typeof retry) => void) | undefined;
  prepareRetry.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        resolvePreparation = resolve;
      }),
  );
  const { result, rerender } = renderHook((value) => usePostgresBackupRestore(value), { initialProps: operation() });
  act(() => {
    result.current.setFile(new File(["SELECT 1;"], "backup.sql"));
    result.current.setConfirmTarget("Main DB 1");
  });
  let pending: Promise<void> | undefined;
  act(() => {
    pending = result.current.restoreBackup({ preventDefault: vi.fn() });
  });
  await waitFor(() => expect(resolvePreparation).toBeTypeOf("function"));
  rerender(operation(2));
  await act(async () => {
    if (!resolvePreparation) throw new Error("Preparation did not start");
    resolvePreparation(retry);
    await pending;
  });
  expect(apiPostForm).not.toHaveBeenCalled();
  expect(reused ? releaseLocalActionRetryAttempt : completeLocalActionRetry).toHaveBeenCalledWith(retry);
  expect(reused ? completeLocalActionRetry : releaseLocalActionRetryAttempt).not.toHaveBeenCalled();
  expect(preserveLocalActionRetryAttempt).not.toHaveBeenCalled();
});

it.each([
  null,
  [],
  {},
  { operation_id: "1", status: "completed", result: {} },
  { operation_id: 0, status: "completed", result: {} },
  { operation_id: 1, status: "running", result: {} },
  { operation_id: 1, status: "completed" },
])("rejects unacknowledged restore envelopes: %j", (response) => {
  expect(() => requireCompletedRestoreResponse(response)).toThrow("Invalid restore completion response");
});

it("accepts a replay acknowledgment without requiring the original result", () => {
  const response = { operation_id: 7, status: "completed", replayed: true };
  expect(requireCompletedRestoreResponse(response)).toBe(response);
});

it.each(["failed", "canceled"])("releases a definitive %s restore attempt", async (status) => {
  const retry = await prepareLocalActionRetry({}, { workspaceID: "workspace-a" });
  await settleRestoreRetryFailure(retry, new APIError("Definitive outcome", { data: { status } }));
  expect(completeLocalActionRetry).toHaveBeenCalledWith(retry);
  expect(preserveLocalActionRetryAttempt).not.toHaveBeenCalled();
});

it("downloads a safe filename and keeps canceled pickers idle", async () => {
  download.mockResolvedValueOnce({ saved: false, canceled: true, method: "picker" });
  const { result } = renderHook(() => usePostgresBackupRestore(operation()));

  await act(async () => result.current.downloadBackup());

  expect(apiDownload).toHaveBeenCalledWith(
    "/api/connector-targets/1/profiles/10/backup",
    "main-db-1.sql",
    expect.objectContaining({ picker: true, requireStreaming: true, signal: expect.any(AbortSignal) }),
  );
  expect(result.current.backupState).toEqual({ state: "idle", error: "", message: "" });
});

it("surfaces backup download failures and leaves the working state", async () => {
  download.mockRejectedValueOnce(new Error("download transport failed"));
  const { result } = renderHook(() => usePostgresBackupRestore(operation()));

  await act(async () => result.current.downloadBackup());

  expect(result.current.backupState).toEqual({ state: "error", error: "download transport failed", message: "" });
  expect(result.current.isWorking).toBe(false);
});

it("discards a backup completion after the selected profile changes", async () => {
  let resolveDownload: ((_result: Awaited<ReturnType<typeof apiDownload>>) => void) | undefined;
  download.mockImplementationOnce(() => new Promise((resolve) => (resolveDownload = resolve)));
  const { result, rerender } = renderHook((value) => usePostgresBackupRestore(value), { initialProps: operation() });
  let downloadPromise: Promise<void> | undefined;
  act(() => {
    downloadPromise = result.current.downloadBackup();
  });
  rerender(operation(2));

  await act(async () => {
    if (!resolveDownload) throw new Error("Download did not start");
    resolveDownload({ saved: true, method: "picker" });
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
  const submitted = postForm.mock.calls[0]?.[1];
  if (!(submitted instanceof FormData)) throw new Error("Restore was not submitted");
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
  let resolveRestore: ((_result: unknown) => void) | undefined;
  postForm.mockImplementationOnce(() => new Promise((resolve) => (resolveRestore = resolve)));
  const { result, rerender } = renderHook((value) => usePostgresBackupRestore(value), { initialProps: operation() });
  act(() => {
    result.current.setFile(new File(["SELECT 1;"], "backup.sql", { type: "text/plain" }));
    result.current.setConfirmTarget("Main DB 1");
  });
  let restorePromise: Promise<void> | undefined;
  act(() => {
    restorePromise = result.current.restoreBackup({ preventDefault: vi.fn() });
  });
  await waitFor(() => expect(resolveRestore).toBeTypeOf("function"));

  rerender(operation(2));
  await act(async () => {
    if (!resolveRestore) throw new Error("Restore did not start");
    resolveRestore({ operation_id: 1, status: "completed", result: {} });
    await restorePromise;
  });

  expect(result.current.restoreState).toEqual({ state: "idle", error: "", message: "" });
  expect(result.current.file).toBeNull();
  expect(result.current.confirmTarget).toBe("");
});

it("reuses one restore identity after an uncertain client failure", async () => {
  postForm.mockRejectedValueOnce(new Error("response lost")).mockResolvedValueOnce({
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
  const first = postForm.mock.calls[0]?.[1];
  const second = postForm.mock.calls[1]?.[1];
  if (!(first instanceof FormData) || !(second instanceof FormData)) throw new Error("Restore attempts were not submitted");
  expect(second.get("idempotency_key")).toBe(first.get("idempotency_key"));
  expect(preserveLocalActionRetryAttempt).toHaveBeenCalledTimes(1);
  expect(completeLocalActionRetry).toHaveBeenCalledTimes(1);
  expect(result.current.restoreState).toEqual({ state: "ready", error: "", message: "Restore completed." });
  expect(result.current.file).toBeNull();
  expect(result.current.confirmTarget).toBe("");
});

it("derives restore retry identity from content instead of mutable file metadata", async () => {
  const first = new File(["SELECT 1;"], "backup.sql", { lastModified: 7 });
  const touched = new File(["SELECT 1;"], "backup.sql", { lastModified: 8 });
  const changed = new File(["SELECT 2;"], "backup.sql", { lastModified: 7 });

  const firstIdentity = await postgresRestoreRetryIdentity("/restore", "Main DB", first);
  const touchedIdentity = await postgresRestoreRetryIdentity("/restore", "Main DB", touched);
  const changedIdentity = await postgresRestoreRetryIdentity("/restore", "Main DB", changed);

  expect(touchedIdentity).toEqual(firstIdentity);
  expect(changedIdentity.body).toMatchObject({ filename: first.name, size: first.size });
  expect(changedIdentity.body.artifact_sha256).not.toBe(firstIdentity.body.artifact_sha256);
  expect(firstIdentity.body).not.toHaveProperty("last_modified");
});

it("records an explicitly uncertain restore without preserving it as a normal retry", async () => {
  const response = { status: "outcome_unknown", operation_id: 7, code: "transport_lost" };
  postForm.mockRejectedValueOnce(new APIError("outcome unknown", { status: 409, code: "transport_lost", data: response }));
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
  postForm.mockRejectedValueOnce(new Error("response lost"));
  preserveAttempt.mockRejectedValueOnce(new Error("ledger unavailable"));
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
  postForm.mockResolvedValueOnce({ ok: true });
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
  postForm.mockRejectedValueOnce(new APIError("different artifact", { status: 409, code: "idempotency_conflict" }));
  const { result } = renderHook(() => usePostgresBackupRestore(operation()));
  act(() => {
    result.current.setFile(new File(["SELECT 2;"], "backup.sql", { type: "text/plain" }));
    result.current.setConfirmTarget("Main DB 1");
  });

  await act(async () => result.current.restoreBackup({ preventDefault: vi.fn() }));

  expect(completeLocalActionRetry).toHaveBeenCalledTimes(1);
  expect(preserveLocalActionRetryAttempt).not.toHaveBeenCalled();
});
