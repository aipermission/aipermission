import { beforeEach, expect, it, vi } from "vitest";
import { apiGet, apiPost } from "../api";
import {
  prepareLocalActionRetry,
  releaseLocalActionRetryAttempt,
  markLocalActionRetryOutcome,
  preserveLocalActionRetryAttempt,
} from "../local-action-retry";
import { resetRetryStorage } from "./storage";
import { mutationTestWorkspace as workspace, setupMutationRetryStorage } from "../../test/connector-mutation-test-state";

const path = "/api/console/bulk-exec";
const body = { target_ids: [7], command: "printf example", reason: "batch admission control", confirmation: "RUN ON 1 TARGETS" };
const request = (changes: Partial<typeof body> = {}) => ({ path, body: { ...body, ...changes } });
const options = { workspaceID: workspace, exclusiveConsoleBatch: true };
setupMutationRetryStorage();
beforeEach(async () => {
  vi.stubGlobal("fetch", async () => json({}));
  await apiGet("/api/status");
});

for (const adapter of ["indexedDB", "memory"] as const) {
  it(`protects same and edited bulk attempts across views through ${adapter}`, async () => {
    await withAdapter(adapter, async () => {
      const first = await prepareLocalActionRetry(request(), options);
      await releaseLocalActionRetryAttempt(first);
      for (const changes of [{}, { command: "edited" }, { target_ids: [9] }])
        await expect(prepareLocalActionRetry(request(changes), options)).rejects.toThrow(/protected retry identity/i);
      const replay = await prepareLocalActionRetry(request(), { workspaceID: workspace });
      expect(replay.idempotencyKey).toBe(first.idempotencyKey);
      await releaseLocalActionRetryAttempt(replay);
      const differentWorkspace = await prepareLocalActionRetry(request(), { ...options, workspaceID: "other-workspace" });
      expect(differentWorkspace.idempotencyKey).not.toBe(first.idempotencyKey);
      await releaseLocalActionRetryAttempt(differentWorkspace);
      const connector = await prepareLocalActionRetry(
        {
          path: "/api/connector-actions/local-run",
          body: {
            target_ref: "example:1:1",
            action_name: "write",
            input: {},
            reason: "independent connector write",
          },
        },
        { workspaceID: workspace, exclusiveMutationActions: ["write"] },
      );
      await releaseLocalActionRetryAttempt(connector);
    });
  });

  it(`retains unknown bulk identity without opening a new external attempt through ${adapter}`, async () => {
    await withAdapter(adapter, async () => {
      const first = await prepareLocalActionRetry(request(), options);
      await markLocalActionRetryOutcome(first, { status: "outcome_unknown" });
      await expect(prepareLocalActionRetry(request(), options)).rejects.toThrow(/protected retry identity/i);
      await expect(prepareLocalActionRetry(request({ command: "edited" }), options)).rejects.toThrow(/protected retry identity/i);
    });
  });

  it(`does not guess that an unattributed older identity belongs to another operation through ${adapter}`, async () => {
    await withAdapter(adapter, async () => {
      const old = await prepareLocalActionRetry({ older_shape: true }, { workspaceID: workspace });
      await releaseLocalActionRetryAttempt(old);
      await expect(prepareLocalActionRetry(request(), options)).rejects.toThrow(/protected retry identity/i);
    });
  });
}

it.each(["same", "edited"])("rejects simultaneous %s batch admissions atomically before another POST", async (mode) => {
  const fetch = vi.fn(async () =>
    json({ parallelism: 3, items: [{ request_id: 41, target_id: 7, target_name: "Example", status: "running" }] }),
  );
  vi.stubGlobal("fetch", fetch);
  const result = await Promise.allSettled([
    apiPost(path, body, { exclusiveConsoleBatch: true }),
    apiPost(path, mode === "same" ? body : { ...body, command: "edited" }, { exclusiveConsoleBatch: true }),
  ]);
  expect(fetch).toHaveBeenCalledOnce();
  expect(result.filter((item) => item.status === "fulfilled")).toHaveLength(1);
  expect(result.filter((item) => item.status === "rejected")).toHaveLength(1);
});

it("permits a fresh batch only after the exact accepted command is verified terminal", async () => {
  const first = await prepareLocalActionRetry(request(), options);
  await preserveLocalActionRetryAttempt(first, {
    parallelism: 3,
    items: [{ request_id: 41, target_id: 7, target_name: "Example", status: "completed" }],
  });
  await expect(prepareLocalActionRetry(request({ command: "new operation" }), options)).rejects.toThrow(/protected retry identity/i);
  vi.stubGlobal("fetch", async () => json({ id: 41, runtime_id: 7, status: "completed" }));
  await apiGet("/api/console/command-requests/41", { workspaceBinding: workspace });
  const next = await prepareLocalActionRetry(request({ command: "new operation" }), options);
  expect(next.idempotencyKey).not.toBe(first.idempotencyKey);
  await releaseLocalActionRetryAttempt(next);
});

async function withAdapter(adapter: "indexedDB" | "memory", action: () => Promise<void>) {
  if (adapter === "indexedDB") return action();
  const browserWindow = window;
  vi.stubGlobal("window", undefined);
  try {
    await action();
  } finally {
    await resetRetryStorage();
    vi.stubGlobal("window", browserWindow);
  }
}
function json(value: unknown) {
  return new Response(JSON.stringify(value), {
    headers: { "Content-Type": "application/json", "X-AIPermission-Workspace": workspace, "X-AIPermission-Workspace-Changed": "true" },
  });
}
