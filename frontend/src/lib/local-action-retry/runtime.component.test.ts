import { afterEach, describe, expect, it, vi } from "vitest";
import { localActionReconciliationEvent } from "./constants.ts";
import { currentRetryScope, readLegacyLedger, removeLegacyLedger, requestReconciliation } from "./runtime.ts";

afterEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
});

describe("retry browser boundary", () => {
  it("keeps legacy reconciliation isolated to the selected workspace", () => {
    const first = currentRetryScope("first");
    const second = currentRetryScope("second");
    localStorage.setItem(first.legacyKey, "unresolved");
    expect(readLegacyLedger(first)).toBe("unresolved");
    expect(readLegacyLedger(second)).toBe("");
    removeLegacyLedger(second);
    expect(readLegacyLedger(first)).toBe("unresolved");
    removeLegacyLedger(first);
    expect(readLegacyLedger(first)).toBe("");
  });

  it("fails closed when legacy storage cannot be read", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("denied");
    });
    expect(() => readLegacyLedger(currentRetryScope("one"))).toThrow("Secure retry storage is unavailable");
  });

  it("defaults reconciliation to refusal without a consumer", async () => {
    await expect(requestReconciliation({ created_at: "2026-09-26" })).resolves.toBe(false);
  });

  it("does not forward malformed persisted metadata into the approval dialog", async () => {
    const handler = (event: Event) => {
      expect(event).toBeInstanceOf(CustomEvent);
      if (!(event instanceof CustomEvent)) return;
      const value: unknown = event.detail;
      expect(value).toBeTypeOf("object");
      if (!value || typeof value !== "object") throw new Error("Missing reconciliation detail");
      const detail = value as Record<string, unknown>;
      expect(detail.requestID).toBeNull();
      expect(detail.operationRef).toBe("");
      expect(detail.assistantHint).toBe("");
      if (typeof detail.resolve !== "function") throw new Error("Missing reconciliation resolver");
      event.preventDefault();
      detail.resolve(true);
      detail.resolve(false);
    };
    window.addEventListener(localActionReconciliationEvent, handler, { once: true });
    await expect(requestReconciliation({ created_at: "2026-09-26", request_id: NaN, operation_ref: [], assistant_hint: {} })).resolves.toBe(
      true,
    );
  });
});
