import { IDBFactory, IDBKeyRange } from "fake-indexeddb";
import { afterEach, beforeEach, expect, vi } from "vitest";
import { act } from "@testing-library/react";
import { resetLocalActionRetryLedger } from "../lib/local-action-retry";
import { scopedUICookieName } from "../lib/ui-cookie";

export const mutationTestWorkspace = "connector-mutation-test-workspace";

export function mutationObservationQueue() {
  let callbacks: (() => Promise<void>)[] = [];
  return {
    schedule(observe: () => Promise<void>, delay: number) {
      expect(delay).toBe(3000);
      callbacks.push(observe);
      return () => {
        callbacks = callbacks.filter((candidate) => candidate !== observe);
      };
    },
    get pending() {
      return callbacks.length;
    },
    clear() {
      callbacks = [];
    },
    async advance() {
      const next = callbacks.shift();
      if (!next) throw new Error("No bounded observation is scheduled");
      await act(async () => next());
    },
  };
}

export function setupMutationRetryStorage() {
  beforeEach(async () => {
    vi.stubGlobal("indexedDB", new IDBFactory());
    vi.stubGlobal("IDBKeyRange", IDBKeyRange);
    document.cookie = `${scopedUICookieName("aipermission_workspace")}=${mutationTestWorkspace}; path=/`;
    await resetLocalActionRetryLedger();
  });
  afterEach(async () => {
    await resetLocalActionRetryLedger();
    vi.unstubAllGlobals();
  });
}
