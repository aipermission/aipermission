import { beforeEach, expect, it, vi } from "vitest";
import { apiGet, apiPost } from "../api";
import {
  completeLocalActionRetry,
  localActionReconciliationEvent,
  markLocalActionRetryOutcome,
  prepareLocalActionRetry,
  preserveLocalActionRetryAttempt,
  releaseLocalActionRetryAttempt,
} from "../local-action-retry";
import { memoryAttempts, openRetryDatabase, requestPromise, resetRetryStorage, transactionPromise } from "./storage";
import { getEntry } from "./entries";
import { connectorActionFixture } from "../../test/connector-action-fixtures";
import { mutationTestWorkspace, setupMutationRetryStorage } from "../../test/connector-mutation-test-state";

const actions = ["write_first", "write_second"];
const body = { target_ref: "example:1:1", action_name: actions[0], input: { value: "first" }, reason: "atomic admission control" };
const request = (overrides: Partial<typeof body> = {}) => ({ path: "/api/connector-actions/local-run", body: { ...body, ...overrides } });
const options = { workspaceID: mutationTestWorkspace, exclusiveMutationActions: actions };
setupMutationRetryStorage();
beforeEach(async () => {
  vi.stubGlobal("fetch", async () => json({}));
  await apiGet("/api/status");
});

for (const adapter of ["indexedDB", "memory"] as const) {
  it(`atomically isolates edited mutations while allowing same-key replays and other profiles through ${adapter}`, async () => {
    await withAdapter(adapter, async () => {
      const first = await prepareLocalActionRetry(request(), options);
      await expect(prepareLocalActionRetry(request(), options)).rejects.toThrow(/protected retry identity/i);
      const second = await prepareLocalActionRetry(request(), { workspaceID: mutationTestWorkspace });
      expect(second.idempotencyKey).toBe(first.idempotencyKey);
      await expect(prepareLocalActionRetry(request({ input: { value: "edited" } }), options)).rejects.toThrow(/protected retry identity/i);
      await expect(prepareLocalActionRetry(request({ action_name: actions[1] }), options)).rejects.toThrow(/protected retry identity/i);
      for (const ref of ["example:1:2", "example:2:1"]) {
        const different = await prepareLocalActionRetry(request({ target_ref: ref }), options);
        expect(different.idempotencyKey).not.toBe(first.idempotencyKey);
        await releaseLocalActionRetryAttempt(different);
      }
      await preserveLocalActionRetryAttempt(first, { request_id: 41, ...body });
      await releaseLocalActionRetryAttempt(second);
    });
  });

  it(`does not classify an attributed read as a competing mutation through ${adapter}`, async () => {
    await withAdapter(adapter, async () => {
      const read = await prepareLocalActionRetry(request({ action_name: "read_value" }), options);
      const write = await prepareLocalActionRetry(request(), options);
      const anotherRead = await prepareLocalActionRetry(request({ action_name: "read_other" }), options);
      expect(read.idempotencyKey).not.toBe(write.idempotencyKey);
      await releaseLocalActionRetryAttempt(read);
      await releaseLocalActionRetryAttempt(write);
      await releaseLocalActionRetryAttempt(anotherRead);
    });
  });

  it(`retains unattributed uncertainty fail-closed instead of guessing another target through ${adapter}`, async () => {
    await withAdapter(adapter, async () => {
      const old = await prepareLocalActionRetry({ old_shape: "unattributed" }, { workspaceID: mutationTestWorkspace });
      await releaseLocalActionRetryAttempt(old);
      await expect(prepareLocalActionRetry(request(), options)).rejects.toThrow(/protected retry identity/i);
    });
  });

  it(`protects attributed older entries before a mutation guard was recorded through ${adapter}`, async () => {
    await withAdapter(adapter, async () => {
      const old = await prepareLocalActionRetry(request(), { workspaceID: mutationTestWorkspace });
      await preserveLocalActionRetryAttempt(old, { request_id: 41, ...body });
      await expect(prepareLocalActionRetry(request({ input: { value: "edited" } }), options)).rejects.toThrow(/protected retry identity/i);
      await expect(prepareLocalActionRetry(request(), options)).rejects.toThrow(/protected retry identity/i);
      const same = await prepareLocalActionRetry(request(), { workspaceID: mutationTestWorkspace });
      expect(same.idempotencyKey).toBe(old.idempotencyKey);
      await releaseLocalActionRetryAttempt(same);
    });
  });

  it(`does not enter reconciliation or replace an unknown guarded identity through ${adapter}`, async () => {
    await withAdapter(adapter, async () => {
      const first = await prepareLocalActionRetry(request(), options);
      await markLocalActionRetryOutcome(first, { status: "outcome_unknown", request_id: 41, ...body });
      const reconciliation = vi.fn();
      const browser = typeof window === "undefined" ? null : window;
      browser?.addEventListener(localActionReconciliationEvent, reconciliation);
      try {
        await expect(prepareLocalActionRetry(request(), options)).rejects.toThrow(/protected retry identity/i);
        expect(reconciliation).not.toHaveBeenCalled();
        expect(await getEntry(first.scope, first.signature)).toMatchObject({ key: first.idempotencyKey, state: "outcome_unknown" });
      } finally {
        browser?.removeEventListener(localActionReconciliationEvent, reconciliation);
      }
    });
  });

  it(`allows a new mutation after a retired identity's abandoned attempt expires through ${adapter}`, async () => {
    await withAdapter(adapter, async () => {
      const first = await prepareLocalActionRetry(request(), options);
      const overlapping = await prepareLocalActionRetry(request(), { workspaceID: mutationTestWorkspace });
      await completeLocalActionRetry(first, true);
      expect(await getEntry(first.scope, first.signature)).toMatchObject({ state: "retired" });
      const expired = new Date(Date.now() - 1000).toISOString();
      if (adapter === "memory") {
        const attempt = memoryAttempts.get(overlapping.attemptID);
        if (!attempt) throw new Error("Overlapping attempt missing");
        memoryAttempts.set(attempt.id, { ...attempt, expires_at: expired });
      } else {
        const database = await openRetryDatabase();
        await transactionPromise(database, "attempts", "readwrite", async (store) => {
          const attempt = await requestPromise(store.get(overlapping.attemptID));
          if (!attempt) throw new Error("Overlapping attempt missing");
          await requestPromise(store.put({ ...attempt, expires_at: expired }));
        });
      }
      const next = await prepareLocalActionRetry(request({ input: { value: "new logical operation" } }), options);
      expect(next.idempotencyKey).not.toBe(first.idempotencyKey);
      await releaseLocalActionRetryAttempt(next);
    });
  });
}

it("does not let a second tab's stale discovery dispatch an edited mutation", async () => {
  let finish!: (_response: Response) => void;
  const post = vi.fn(
    () =>
      new Promise<Response>((resolve) => {
        finish = resolve;
      }),
  );
  vi.stubGlobal("fetch", post);
  const first = apiPost("/api/connector-actions/local-run", body, { exclusiveMutationActions: actions });
  await vi.waitFor(() => expect(post).toHaveBeenCalledOnce());
  await expect(
    apiPost("/api/connector-actions/local-run", { ...body, input: { value: "edited" } }, { exclusiveMutationActions: actions }),
  ).rejects.toThrow(/protected retry identity/i);
  expect(post).toHaveBeenCalledOnce();
  finish(json(connectorActionFixture({ status: "running", request_id: 41, target_ref: body.target_ref, action_name: body.action_name })));
  await first;
});

it.each(["same", "edited"])("transactionally rejects simultaneous %s exclusive admissions before another POST", async (mode) => {
  const post = vi.fn(async () =>
    json(connectorActionFixture({ status: "running", request_id: 41, target_ref: body.target_ref, action_name: body.action_name })),
  );
  vi.stubGlobal("fetch", post);
  const outcomes = await Promise.allSettled([
    apiPost("/api/connector-actions/local-run", body, { exclusiveMutationActions: actions }),
    apiPost("/api/connector-actions/local-run", mode === "same" ? body : { ...body, input: { value: "edited" } }, {
      exclusiveMutationActions: actions,
    }),
  ]);
  expect(post).toHaveBeenCalledOnce();
  expect(outcomes.filter((item) => item.status === "fulfilled")).toHaveLength(1);
  expect(outcomes.filter((item) => item.status === "rejected")).toHaveLength(1);
});

async function withAdapter(adapter: "indexedDB" | "memory", test: () => Promise<void>) {
  if (adapter === "indexedDB") return test();
  const browserWindow = window;
  vi.stubGlobal("window", undefined);
  try {
    await test();
  } finally {
    await resetRetryStorage();
    vi.stubGlobal("window", browserWindow);
  }
}

function json(value: unknown) {
  return new Response(JSON.stringify(value), {
    headers: {
      "Content-Type": "application/json",
      "X-AIPermission-Workspace": mutationTestWorkspace,
      "X-AIPermission-Workspace-Changed": "true",
    },
  });
}
