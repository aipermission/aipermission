import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiPost, currentWorkspaceBinding } from "../../../../lib/api";
import { roleHistoryPageFixture, deferredRoleHistoryReply as deferred } from "../../../../test/postgres/role-history-fixtures.test";
import { useRoleHistory } from "./use-role-history";

vi.mock("../../../../lib/api", () => ({ apiPost: vi.fn(), currentWorkspaceBinding: vi.fn() }));
const post = vi.mocked(apiPost);
const binding = vi.mocked(currentWorkspaceBinding);
function page(targetID = 1) {
  const value = roleHistoryPageFixture(targetID);
  value.entries[0]!.record.status = "cleanup_intent";
  return value;
}
function receipt() {
  const entry = page().entries[0]!;
  entry.record.status = "provisioned";
  entry.record.generation = "c".repeat(32);
  return { target_id: 1, entry, evidence: "exact_remote_identity_present" };
}
async function ready() {
  const hook = renderHook((id) => useRoleHistory(id), { initialProps: 1 });
  await waitFor(() => expect(hook.result.current.state).toBe("ready"));
  return hook;
}
beforeEach(() => {
  post.mockReset().mockResolvedValue(page());
  binding.mockReset().mockReturnValue("workspace-a");
});

it("submits the exact current evidence once and freezes reads until its mandatory reload completes", async () => {
  const { result } = await ready();
  const entry = result.current.page!.entries[0]!;
  const old = result.current;
  const mutation = deferred();
  const reload = deferred();
  post.mockReturnValueOnce(mutation.promise).mockReturnValueOnce(reload.promise);
  let action!: Promise<void>;
  act(() => {
    action = old.reconcile(entry, 2);
    void old.reconcile(entry, 2);
  });
  expect(result.current.busy).toBe(true);
  expect(post).toHaveBeenCalledTimes(2);
  expect(post.mock.calls[1]).toEqual([
    "/api/connector-targets/1/operations/role-lifecycle-reconcile",
    { profile_id: "2", input: { expected: entry } },
    { signal: expect.any(AbortSignal), workspaceBinding: "workspace-a" },
  ]);
  await act(async () => {
    await old.refresh();
    await old.next();
    await old.previous();
    await old.first();
  });
  expect(post).toHaveBeenCalledTimes(2);
  await act(async () => {
    mutation.resolve(receipt());
    await mutation.promise;
  });
  expect(result.current.state).toBe("loading");
  expect(result.current.busy).toBe(true);
  await act(async () => {
    reload.resolve({ ...page(), entries: [receipt().entry] });
    await action;
  });
  expect(result.current.busy).toBe(false);
  expect(result.current.notice).toBe("Exact role presence confirmed.");
  expect(result.current.page!.entries[0]!.record.status).toBe("provisioned");
  expect(post).toHaveBeenCalledTimes(3);
});

it.each(["error", "invalidReceipt"])("reloads evidence without retrying an uncertain %s", async (kind) => {
  const { result } = await ready();
  if (kind === "error") post.mockRejectedValueOnce(new Error("private-secret-in-transport"));
  else post.mockResolvedValueOnce({});
  post.mockResolvedValueOnce(page());
  await act(async () => {
    await result.current.reconcile(result.current.page!.entries[0]!, 2);
  });
  expect(result.current.notice).toMatch(/^Decision outcome was not confirmed/);
  expect(result.current.notice).not.toContain("private-secret");
  expect(result.current.busy).toBe(false);
  expect(result.current.state).toBe("ready");
  expect(post).toHaveBeenCalledTimes(3);
  expect(post.mock.calls.filter(([path]) => path.endsWith("role-lifecycle-reconcile"))).toHaveLength(1);
});

it("blocks another decision when the evidence reload fails", async () => {
  const { result } = await ready();
  const old = result.current;
  const entry = old.page!.entries[0]!;
  post.mockResolvedValueOnce(receipt()).mockRejectedValueOnce(new Error("read failed"));
  await act(async () => {
    await old.reconcile(entry, 2);
  });
  expect(result.current.state).toBe("error");
  expect(result.current.page).toBeNull();
  expect(result.current.busy).toBe(false);
  await act(async () => {
    await old.reconcile(entry, 2);
  });
  expect(post).toHaveBeenCalledTimes(3);
});

it.each([0, 3, Number.NaN, 2.5])("rejects unbound admin profile %s before dispatch", async (id) => {
  const { result } = await ready();
  await act(async () => {
    await result.current.reconcile(result.current.page!.entries[0]!, id);
  });
  expect(post).toHaveBeenCalledTimes(1);
});

it("rejects cloned evidence and callbacks retained across a refresh", async () => {
  const { result } = await ready();
  const old = result.current;
  const entry = old.page!.entries[0]!;
  await act(async () => {
    await old.reconcile(structuredClone(entry), 2);
    await old.refresh();
    await old.reconcile(entry, 2);
  });
  expect(post).toHaveBeenCalledTimes(2);
});

it("rejects a retained decision after unmount before dispatch", async () => {
  const { result, unmount } = await ready();
  const old = result.current;
  const entry = old.page!.entries[0]!;
  unmount();
  await act(async () => {
    await old.reconcile(entry, 2);
  });
  expect(post).toHaveBeenCalledTimes(1);
});

it.each(["resolve", "reject"])("ignores a late decision %s after unmount", async (kind) => {
  const { result, unmount } = await ready();
  const pending = deferred();
  post.mockReturnValueOnce(pending.promise);
  let action!: Promise<void>;
  act(() => {
    action = result.current.reconcile(result.current.page!.entries[0]!, 2);
  });
  const signal = post.mock.calls[1][2]!.signal!;
  unmount();
  expect(signal.aborted).toBe(true);
  await act(async () => {
    if (kind === "resolve") pending.resolve(receipt());
    else pending.reject(new Error("late"));
    await action;
  });
  expect(post).toHaveBeenCalledTimes(2);
});

it("does not revive a pending decision or its callback on target ABA", async () => {
  const { result, rerender } = await ready();
  const old = result.current;
  const entry = old.page!.entries[0]!;
  const pending = deferred();
  post.mockReturnValueOnce(pending.promise).mockResolvedValueOnce(page(2)).mockResolvedValueOnce(page());
  let action!: Promise<void>;
  act(() => {
    action = old.reconcile(entry, 2);
  });
  const signal = post.mock.calls[1][2]!.signal!;
  rerender(2);
  await waitFor(() => expect(result.current.page?.target_id).toBe(2));
  expect(result.current.busy).toBe(false);
  rerender(1);
  await waitFor(() => expect(result.current.page?.target_id).toBe(1));
  await act(async () => {
    pending.resolve(receipt());
    await action;
    await old.reconcile(entry, 2);
  });
  expect(signal.aborted).toBe(true);
  expect(result.current.notice).toBe("");
  expect(post).toHaveBeenCalledTimes(4);
});

it("invalidates a pending decision on workspace drift, without restoring it on ABA", async () => {
  const { result, rerender } = await ready();
  const pending = deferred();
  post.mockReturnValueOnce(pending.promise);
  let action!: Promise<void>;
  act(() => {
    action = result.current.reconcile(result.current.page!.entries[0]!, 2);
  });
  const signal = post.mock.calls[1][2]!.signal!;
  binding.mockReturnValue("workspace-b");
  rerender(1);
  expect(result.current.workspaceChanged).toBe(true);
  expect(result.current.busy).toBe(false);
  binding.mockReturnValue("workspace-a");
  await act(async () => {
    pending.resolve(receipt());
    await action;
  });
  expect(result.current.workspaceChanged).toBe(true);
  expect(signal.aborted).toBe(true);
  expect(result.current.notice).toBe("");
  expect(post).toHaveBeenCalledTimes(2);
});

it("cleans an exact provisioned role once, reloads evidence and does not delete a local credential", async () => {
  const provisioned = page();
  provisioned.entries[0]!.record.status = "provisioned";
  post.mockResolvedValueOnce(provisioned);
  const { result } = await ready();
  const entry = result.current.page!.entries[0]!;
  const old = result.current;
  const cleaned = structuredClone(entry);
  cleaned.record.status = "cleaned";
  cleaned.record.generation = "d".repeat(32);
  const mutation = deferred();
  post.mockReturnValueOnce(mutation.promise).mockResolvedValueOnce({ ...provisioned, entries: [cleaned] });
  await act(async () => {
    await old.cleanup(entry, 2);
    await old.cleanup(entry, 2, `${entry.record.intent.role_name} `);
    await old.cleanup(entry, 3, entry.record.intent.role_name);
    await old.reconcile(entry, 2);
  });
  expect(post).toHaveBeenCalledTimes(1);
  let action!: Promise<void>;
  act(() => {
    action = old.cleanup(entry, 2, entry.record.intent.role_name);
    void old.cleanup(entry, 2, entry.record.intent.role_name);
  });
  expect(result.current.busy).toBe(true);
  expect(post.mock.calls[1]).toEqual([
    "/api/connector-targets/1/operations/role-lifecycle-cleanup",
    { profile_id: "2", input: { expected: entry, confirmed_role_name: entry.record.intent.role_name } },
    { signal: expect.any(AbortSignal), workspaceBinding: "workspace-a" },
  ]);
  await act(async () => {
    mutation.resolve({ target_id: 1, entry: cleaned, evidence: "acknowledged_remote_cleanup" });
    await action;
    await old.cleanup(entry, 2, entry.record.intent.role_name);
  });
  expect(result.current.notice).toBe("Remote role cleanup acknowledged. Local credentials were not changed.");
  expect(result.current.page!.entries[0]!.record.status).toBe("cleaned");
  expect(post).toHaveBeenCalledTimes(3);
});

it("does not retry unknown cleanup and rejects its retained callback after target ABA", async () => {
  const provisioned = page();
  provisioned.entries[0]!.record.status = "provisioned";
  post.mockResolvedValueOnce(provisioned);
  const { result, rerender } = await ready();
  const old = result.current;
  const entry = old.page!.entries[0]!;
  const mutation = deferred();
  post.mockReturnValueOnce(mutation.promise).mockResolvedValueOnce(page(2)).mockResolvedValueOnce(provisioned);
  let action!: Promise<void>;
  act(() => {
    action = old.cleanup(entry, 2, entry.record.intent.role_name);
  });
  const signal = post.mock.calls[1][2]!.signal!;
  rerender(2);
  await waitFor(() => expect(result.current.page?.target_id).toBe(2));
  rerender(1);
  await waitFor(() => expect(result.current.page?.target_id).toBe(1));
  await act(async () => {
    mutation.reject(new Error("lost cleanup acknowledgement"));
    await action;
    await old.cleanup(entry, 2, entry.record.intent.role_name);
  });
  expect(signal.aborted).toBe(true);
  expect(result.current.notice).toBe("");
  expect(post.mock.calls.filter(([path]) => path.endsWith("role-lifecycle-cleanup"))).toHaveLength(1);
});
