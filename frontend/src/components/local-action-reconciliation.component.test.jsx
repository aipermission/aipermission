import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { IDBFactory } from "fake-indexeddb";
import { describe, expect, it, vi } from "vitest";
import { scopedUICookieName } from "../lib/ui-cookie";
import {
  completeLocalActionRetry,
  listLocalActionRetryEntries,
  markLocalActionRetryOutcome,
  prepareLocalActionRetry,
  resetLocalActionRetryLedger,
  resolveLocalActionRetryEntry,
} from "../lib/local-action-retry";
import { LocalActionReconciliationDialog } from "./local-action-reconciliation-dialog";
import { LocalActionRetryPanel } from "./settings/local-action-retry-panel";

describe("local connector action reconciliation", () => {
  it("keeps an unknown outcome protected until the operator explicitly starts a new attempt", async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();
    render(<LocalActionReconciliationDialog value={{ requestID: 91, assistantHint: "Inspect external state first." }} onClose={onClose} />);

    expect(screen.getByText(/may repeat an operation that already completed/i)).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Keep protected" }));
    expect(onClose).toHaveBeenLastCalledWith(false);

    await user.click(screen.getByRole("button", { name: "Start new attempt" }));
    expect(onClose).toHaveBeenLastCalledWith(true);
  });

  it("requires explicit reset confirmation before removing a legacy retry ledger", async () => {
    const originalIndexedDB = globalThis.indexedDB;
    globalThis.indexedDB = new IDBFactory();
    const user = userEvent.setup();
    document.cookie = `${scopedUICookieName("aipermission_workspace")}=test-workspace; Path=/`;
    window.localStorage.setItem(
      "aipermission.local-action-retry.v2.test-workspace",
      JSON.stringify({
        version: 2,
        entries: {
          signature: {
            key: "retry-key",
            state: "outcome_unknown",
            request_id: 91,
            created_at: "2026-09-04T10:00:00Z",
            updated_at: "2026-09-04T10:00:00Z",
          },
        },
      }),
    );
    render(<LocalActionRetryPanel />);

    expect(await screen.findByText("Outcome unknown")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Mark retry identity reconciled" }));
    expect(screen.getByText(/protected local retry identities will be lost/i)).toBeVisible();
    expect(window.localStorage.getItem("aipermission.local-action-retry.v2.test-workspace")).not.toBeNull();

    await user.click(screen.getByRole("button", { name: "Reset ledger" }));
    expect(await screen.findByText("No unresolved local connector attempts.")).toBeVisible();
    expect(window.localStorage.getItem("aipermission.local-action-retry.v2.test-workspace")).toBeNull();
    if (originalIndexedDB === undefined) delete globalThis.indexedDB;
    else globalThis.indexedDB = originalIndexedDB;
  });

  it("preserves retry identity until completion or explicit outcome reconciliation", async () => {
    const originalIndexedDB = globalThis.indexedDB;
    globalThis.indexedDB = new IDBFactory();
    document.cookie = `${scopedUICookieName("aipermission_workspace")}=retry-lifecycle; Path=/`;
    try {
      await resetLocalActionRetryLedger();
      const body = { target_ref: "fixture:1:1", action_name: "mutate", input: { value: 1 } };
      const prepared = await prepareLocalActionRetry(body);
      const reused = await prepareLocalActionRetry(body);

      expect(prepared.reused).toBe(false);
      expect(reused.reused).toBe(true);
      expect(reused.idempotencyKey).toBe(prepared.idempotencyKey);
      expect(await listLocalActionRetryEntries()).toHaveLength(1);
      expect(await resolveLocalActionRetryEntry((await listLocalActionRetryEntries())[0])).toBe(false);

      await completeLocalActionRetry(reused);
      expect(await listLocalActionRetryEntries()).toHaveLength(1);
      await completeLocalActionRetry(prepared);
      expect(await listLocalActionRetryEntries()).toEqual([]);

      const uncertain = await prepareLocalActionRetry({ ...body, input: { value: 2 } });
      await markLocalActionRetryOutcome(uncertain, { request_id: 91, assistant_hint: "Inspect external state." });
      const [entry] = await listLocalActionRetryEntries();
      expect(entry).toMatchObject({ state: "outcome_unknown", request_id: 91, assistant_hint: "Inspect external state." });
      expect(await resolveLocalActionRetryEntry(entry)).toBe(true);
      expect(await listLocalActionRetryEntries()).toEqual([]);
    } finally {
      await resetLocalActionRetryLedger();
      if (originalIndexedDB === undefined) delete globalThis.indexedDB;
      else globalThis.indexedDB = originalIndexedDB;
    }
  });
});
