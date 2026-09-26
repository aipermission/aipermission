import { IDBFactory, IDBKeyRange } from "fake-indexeddb";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { APIError } from "../../../lib/errors";
import {
  completeLocalActionRetry,
  listLocalActionRetryEntries,
  prepareLocalActionRetry,
  resetLocalActionRetryLedger,
} from "../../../lib/local-action-retry";
import { scopedUICookieName } from "../../../lib/ui-cookie";
import { settleRestoreRetryFailure } from "./use-postgres-backup-restore";

beforeEach(async () => {
  vi.stubGlobal("indexedDB", new IDBFactory());
  vi.stubGlobal("IDBKeyRange", IDBKeyRange);
  document.cookie = `${scopedUICookieName("aipermission_workspace")}=postgres-restore-retry; path=/`;
  await resetLocalActionRetryLedger();
});

afterEach(async () => {
  await resetLocalActionRetryLedger();
  vi.unstubAllGlobals();
});

function restoreIdentity() {
  return {
    path: "/api/connector-targets/1/profiles/2/restore",
    body: { confirm_target: "Main DB", filename: "backup.sql", size: 9, artifact_sha256: "content-sha256" },
  };
}

it("persists an uncertain restore in the real reconciliation ledger", async () => {
  const prepared = await prepareLocalActionRetry(restoreIdentity());
  await settleRestoreRetryFailure(
    prepared,
    new APIError("restore outcome unknown", {
      status: 409,
      code: "transport_lost",
      data: { operation_id: 19, status: "outcome_unknown" },
    }),
  );

  const [entry] = await listLocalActionRetryEntries();
  expect(entry).toMatchObject({ key: prepared.idempotencyKey, state: "outcome_unknown", operation_ref: "operation:19" });
});

it("retires a definitive restore rejection in the real reconciliation ledger", async () => {
  const prepared = await prepareLocalActionRetry(restoreIdentity());
  await settleRestoreRetryFailure(prepared, new APIError("invalid artifact", { status: 400, code: "invalid_restore" }));

  await expect(listLocalActionRetryEntries()).resolves.toEqual([]);
});

it("retires a terminal connector failure even when the gateway returns 502", async () => {
  const prepared = await prepareLocalActionRetry(restoreIdentity());
  await settleRestoreRetryFailure(
    prepared,
    new APIError("connector restore failed", {
      status: 502,
      code: "invalid_connector_result",
      data: { operation_id: 21, status: "failed" },
    }),
  );

  await expect(listLocalActionRetryEntries()).resolves.toEqual([]);
});

it("preserves a reused restore identity across unrelated pre-dispatch 4xx failures", async () => {
  const first = await prepareLocalActionRetry(restoreIdentity());
  await settleRestoreRetryFailure(first, new Error("restore response was lost"));

  const reconciliation = await prepareLocalActionRetry(restoreIdentity());
  expect(reconciliation).toMatchObject({ idempotencyKey: first.idempotencyKey, reused: true });
  await settleRestoreRetryFailure(reconciliation, new APIError("lifecycle gate timed out", { status: 408, code: "request_timeout" }));

  const replay = await prepareLocalActionRetry(restoreIdentity());
  expect(replay).toMatchObject({ idempotencyKey: first.idempotencyKey, reused: true });
  await completeLocalActionRetry(replay);
  await expect(listLocalActionRetryEntries()).resolves.toEqual([]);
});
