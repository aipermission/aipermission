import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiPost } from "../../../lib/api";
import { cleanupFixture, cleanupTargetID } from "./cleanup-test-fixtures";
import { useCleanupReconciliation } from "./use-cleanup-reconciliation";

const workspace = vi.hoisted(() => ({ value: "workspace-a" }));
vi.mock("../../../lib/api", () => ({ apiPost: vi.fn(), currentWorkspaceBinding: () => workspace.value }));

function deferred() {
  let resolve!: (_value: unknown) => void;
  let reject!: (_error: Error) => void;
  const promise = new Promise<unknown>((done, fail) => {
    resolve = done;
    reject = fail;
  });
  return { promise, resolve, reject };
}

function wireFor(targetID: number) {
  return { ...cleanupFixture().wire, target_id: targetID };
}

beforeEach(() => {
  workspace.value = "workspace-a";
  vi.mocked(apiPost).mockReset().mockResolvedValue(cleanupFixture().wire);
});

async function loaded() {
  const view = renderHook(() => useCleanupReconciliation(cleanupTargetID));
  await waitFor(() => expect(view.result.current.phase).toBe("ready"));
  return view;
}

describe("SSH cleanup observations", () => {
  it("loads only public target evidence and aborts its request on unmount", async () => {
    const reading = deferred();
    vi.mocked(apiPost).mockReturnValue(reading.promise);
    const view = renderHook(() => useCleanupReconciliation(cleanupTargetID));
    expect(view.result.current.phase).toBe("loading");
    expect(view.result.current.snapshot).toBeNull();
    expect(apiPost).toHaveBeenCalledWith(
      "/api/connector-targets/7/operations/key-cleanup-status",
      {},
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
    const signal = vi.mocked(apiPost).mock.calls[0][2]!.signal!;
    view.unmount();
    expect(signal.aborted).toBe(true);
    await act(async () => reading.resolve(cleanupFixture().wire));
    expect(apiPost).toHaveBeenCalledOnce();
  });

  it.each([null, { target_id: 8 }, { error: "wrong contract" }])("rejects invalid status %j without claiming absence", async (response) => {
    vi.mocked(apiPost).mockResolvedValue(response);
    const { result } = renderHook(() => useCleanupReconciliation(cleanupTargetID));
    await waitFor(() => expect(result.current.phase).toBe("error"));
    expect(result.current.snapshot).toBeNull();
    expect(result.current.error).toMatch(/Invalid SSH/);
    expect(result.current.notice).toBe("");
  });

  it("recovers an observation error through explicit refresh", async () => {
    vi.mocked(apiPost).mockRejectedValueOnce(new Error("Observation unavailable"));
    const { result } = renderHook(() => useCleanupReconciliation(cleanupTargetID));
    await waitFor(() => expect(result.current.phase).toBe("error"));
    expect(result.current.error).toBe("Observation unavailable");
    await act(async () => result.current.refresh());
    expect(result.current.phase).toBe("ready");
    expect(result.current.error).toBe("");
    expect(result.current.revision).toBe(1);
  });

  it.each(["resolve", "reject"] as const)("ignores the retired target read's %s", async (settlement) => {
    const old = deferred();
    const next = deferred();
    vi.mocked(apiPost).mockReturnValueOnce(old.promise).mockReturnValueOnce(next.promise);
    const { result, rerender } = renderHook((id) => useCleanupReconciliation(id), { initialProps: 7 });
    const signal = vi.mocked(apiPost).mock.calls[0][2]!.signal!;
    rerender(8);
    expect(signal.aborted).toBe(true);
    await act(async () => (settlement === "resolve" ? old.resolve(wireFor(7)) : old.reject(new Error("retired error"))));
    expect(result.current.phase).toBe("loading");
    expect(result.current.snapshot).toBeNull();
    expect(result.current.error).toBe("");
    await act(async () => next.resolve(wireFor(8)));
    expect(result.current.snapshot?.target_id).toBe(8);
    expect(result.current.phase).toBe("ready");
  });
});

describe("SSH cleanup mutation outcome", () => {
  it.each(["generation", "deletion_context_digest", "identity_digest", "resource_id"] as const)(
    "rejects a locally stale %s before dispatch",
    async (field) => {
      const { result } = await loaded();
      const input = {
        ...cleanupFixture().submission,
        [field]: field === "resource_id" ? 99 : "f".repeat(field === "generation" ? 32 : 64),
      };
      await act(async () => result.current.submit(input));
      expect(apiPost).toHaveBeenCalledOnce();
      expect(result.current.error).toMatch(/Reload SSH cleanup evidence/);
      expect(result.current.phase).toBe("ready");
    },
  );

  it("records exactly once, blocks synchronous duplicates and refresh, then reloads current evidence", async () => {
    const { result } = await loaded();
    const { submission, acknowledgement, wire } = cleanupFixture();
    const mutation = deferred();
    vi.mocked(apiPost)
      .mockReturnValueOnce(mutation.promise)
      .mockResolvedValueOnce({ ...wire, records: [{ ...wire.records[0], entry: acknowledgement.entry }] });
    let sending!: Promise<void>;
    act(() => {
      sending = result.current.submit(submission);
      void result.current.submit(submission);
      void result.current.refresh();
    });
    expect(result.current.phase).toBe("submitting");
    expect(apiPost).toHaveBeenCalledTimes(2);
    expect(apiPost).toHaveBeenLastCalledWith(
      "/api/connector-targets/7/operations/key-cleanup-attest",
      submission,
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
    await act(async () => {
      mutation.resolve(acknowledgement);
      await sending;
    });
    expect(apiPost).toHaveBeenCalledTimes(3);
    expect(vi.mocked(apiPost).mock.calls.map(([path]) => path)).toEqual([
      "/api/connector-targets/7/operations/key-cleanup-status",
      "/api/connector-targets/7/operations/key-cleanup-attest",
      "/api/connector-targets/7/operations/key-cleanup-status",
    ]);
    expect(result.current.snapshot?.records[0].entry.record.generation).toBe(acknowledgement.entry.record.generation);
    expect(result.current.notice).toBe("Decision recorded. Current cleanup evidence reloaded.");
    expect(result.current.phase).toBe("ready");
    expect(result.current.revision).toBe(2);
  });

  it.each(["conflict", "reply loss", "invalid acknowledgement"])("observes after %s, never resends the mutation", async (outcome) => {
    const { result } = await loaded();
    const { submission, wire, acknowledgement } = cleanupFixture();
    if (outcome === "invalid acknowledgement") vi.mocked(apiPost).mockResolvedValueOnce({ ok: true });
    else vi.mocked(apiPost).mockRejectedValueOnce(new Error(outcome));
    vi.mocked(apiPost).mockResolvedValueOnce({ ...wire, records: [{ ...wire.records[0], entry: acknowledgement.entry }] });
    await act(async () => result.current.submit(submission));
    expect(vi.mocked(apiPost).mock.calls.filter(([path]) => path.endsWith("key-cleanup-attest"))).toHaveLength(1);
    expect(result.current.notice).toMatch(/outcome was not confirmed/);
    expect(result.current.notice).toMatch(/No automatic retry/);
    expect(result.current.notice).not.toMatch(/Decision recorded/);
    expect(result.current.snapshot?.records[0].entry.record.status).toBe("attested");
    expect(result.current.phase).toBe("ready");
  });

  it.each([true, false])("keeps a reload failure separate from acknowledged=%s", async (acknowledged) => {
    const { result } = await loaded();
    const { submission, acknowledgement } = cleanupFixture();
    if (acknowledged) vi.mocked(apiPost).mockResolvedValueOnce(acknowledgement);
    else vi.mocked(apiPost).mockRejectedValueOnce(new Error("reply lost"));
    vi.mocked(apiPost).mockRejectedValueOnce(new Error("read failed"));
    await act(async () => result.current.submit(submission));
    expect(result.current.phase).toBe("error");
    expect(result.current.snapshot).toBeNull();
    expect(result.current.error).toContain("read failed");
    expect(result.current.notice).toMatch(acknowledged ? /was acknowledged/ : /outcome was not confirmed/);
    await act(async () => result.current.refresh());
    expect(result.current.phase).toBe("ready");
    expect(vi.mocked(apiPost).mock.calls.filter(([path]) => path.endsWith("key-cleanup-attest"))).toHaveLength(1);
  });
});

describe("SSH cleanup workspace ownership", () => {
  it("does not submit a cached selection after a database switch", async () => {
    const { result } = await loaded();
    workspace.value = "workspace-b";
    await act(async () => result.current.submit(cleanupFixture().submission));
    expect(apiPost).toHaveBeenCalledOnce();
    expect(result.current.phase).toBe("error");
    expect(result.current.snapshot).toBeNull();
    expect(result.current.error).toMatch(/database changed/);
    await act(async () => result.current.refresh());
    expect(result.current.phase).toBe("ready");
  });

  it("does not install status returned for a retired database", async () => {
    const old = deferred();
    vi.mocked(apiPost).mockReturnValueOnce(old.promise);
    const { result } = renderHook(() => useCleanupReconciliation(7));
    workspace.value = "workspace-b";
    await act(async () => old.resolve(wireFor(7)));
    expect(result.current.snapshot).toBeNull();
    expect(result.current.notice).toBe("");
    expect(result.current.phase).toBe("error");
    expect(result.current.error).toMatch(/database changed/);
    await act(async () => result.current.refresh());
    expect(result.current.phase).toBe("ready");
  });

  it.each(["resolve", "reject"] as const)("never starts a foreign-database observation after old mutation %s", async (settlement) => {
    const { result } = await loaded();
    const mutation = deferred();
    vi.mocked(apiPost).mockReturnValueOnce(mutation.promise);
    let sending!: Promise<void>;
    act(() => {
      sending = result.current.submit(cleanupFixture().submission);
    });
    workspace.value = "workspace-b";
    await act(async () => {
      if (settlement === "resolve") mutation.resolve(cleanupFixture().acknowledgement);
      else mutation.reject(new Error("old error"));
      await sending;
    });
    expect(apiPost).toHaveBeenCalledTimes(2);
    expect(result.current.notice).toBe("");
    expect(result.current.error).toMatch(/original database/);
    expect(result.current.phase).toBe("error");
    expect(result.current.snapshot).toBeNull();
    await act(async () => result.current.refresh());
    expect(result.current.phase).toBe("ready");
  });

  it("does not release a new target mutation when an old mutation settles", async () => {
    const { result, rerender } = renderHook((id) => useCleanupReconciliation(id), { initialProps: 7 });
    await waitFor(() => expect(result.current.phase).toBe("ready"));
    const old = deferred();
    const next = deferred();
    vi.mocked(apiPost)
      .mockReturnValueOnce(old.promise)
      .mockResolvedValueOnce(wireFor(8))
      .mockReturnValueOnce(next.promise)
      .mockResolvedValueOnce(wireFor(8));
    let first!: Promise<void>;
    act(() => {
      first = result.current.submit(cleanupFixture().submission);
    });
    const oldSignal = vi.mocked(apiPost).mock.calls[1][2]!.signal!;
    rerender(8);
    await waitFor(() => expect(result.current.snapshot?.target_id).toBe(8));
    let second!: Promise<void>;
    act(() => {
      second = result.current.submit(cleanupFixture().submission);
    });
    await act(async () => {
      old.resolve(cleanupFixture().acknowledgement);
      await first;
    });
    expect(oldSignal.aborted).toBe(true);
    expect(result.current.phase).toBe("submitting");
    await act(async () => result.current.refresh());
    expect(apiPost).toHaveBeenCalledTimes(4);
    await act(async () => {
      next.resolve(cleanupFixture().acknowledgement);
      await second;
    });
    expect(result.current.snapshot?.target_id).toBe(8);
    expect(result.current.phase).toBe("ready");
  });

  it("aborts a closed dialog mutation without rereading or retrying", async () => {
    const view = await loaded();
    const mutation = deferred();
    vi.mocked(apiPost).mockReturnValueOnce(mutation.promise);
    let sending!: Promise<void>;
    act(() => {
      sending = view.result.current.submit(cleanupFixture().submission);
    });
    const signal = vi.mocked(apiPost).mock.calls[1][2]!.signal!;
    view.unmount();
    expect(signal.aborted).toBe(true);
    await act(async () => {
      mutation.resolve(cleanupFixture().acknowledgement);
      await sending;
    });
    expect(apiPost).toHaveBeenCalledTimes(2);
  });
});
