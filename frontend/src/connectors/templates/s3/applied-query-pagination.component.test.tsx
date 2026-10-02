import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiPost } from "../../../lib/api";
import { connectorActionRequest } from "../../../test/connector-action-fixtures";
import { useS3Browser } from "./use-s3-browser";

vi.mock("../../../lib/api", () => ({ apiPost: vi.fn(), saveBlob: vi.fn() }));

function response(objects: { key: string }[], cursor: string, targetRef = "s3:1:1") {
  return {
    status: "completed",
    request_id: 1,
    target_ref: targetRef,
    connector_kind: "s3",
    action_name: "list_objects",
    retry_policy: { class: "read_only", guidance: "Read again." },
    output: { objects, directories: [], next_cursor: cursor },
  };
}

beforeEach(() => {
  vi.mocked(apiPost)
    .mockReset()
    .mockResolvedValue(response([{ key: "first.txt" }], "root-cursor"));
});

async function loadedBrowser() {
  const hook = renderHook(() => useS3Browser({ target: { ref: "s3:1:1" }, session: { active: true, startedAt: "now" } }));
  await waitFor(() => expect(hook.result.current.nextToken).toBe("root-cursor"));
  return hook;
}

it.each([
  { prefix: "new/", search: "" },
  { prefix: "", search: "new search" },
  { prefix: "new/", search: "new search" },
])("keeps a continuation bound to the successful query rather than $prefix/$search drafts", async (draft) => {
  const { result } = await loadedBrowser();
  act(() => {
    result.current.setPrefix("old/");
    result.current.setSearch("old search");
  });
  vi.mocked(apiPost).mockResolvedValueOnce(response([{ key: "old/first.txt" }], "old-cursor"));
  await act(async () => result.current.refreshObjects({ reset: true }));
  act(() => {
    result.current.setPrefix(draft.prefix);
    result.current.setSearch(draft.search);
  });
  vi.mocked(apiPost).mockResolvedValueOnce(response([{ key: "old/second.txt" }], "last-cursor"));
  await act(async () => result.current.refreshObjects({ reset: false, token: result.current.nextToken }));
  expect(connectorActionRequest(vi.mocked(apiPost).mock.lastCall![1]).input).toEqual({
    prefix: "old/",
    search: "old search",
    cursor: "old-cursor",
    limit: 100,
  });
  expect(result.current.objects.map((object) => object.key)).toEqual(["old/first.txt", "old/second.txt"]);
  expect(result.current.prefix).toBe(draft.prefix);
  expect(result.current.search).toBe(draft.search);
  expect(result.current.appliedQuery).toEqual({ prefix: "old/", search: "old search" });
});

it.each(["completed", "rejected", "pending"])("retires an old continuation while a new query is loading and after %s", async (outcome) => {
  const { result } = await loadedBrowser();
  const oldMore = () => result.current.refreshObjects({ reset: false, token: "root-cursor" });
  let resolve!: (_value: unknown) => void;
  let reject!: (_error: Error) => void;
  vi.mocked(apiPost).mockReturnValueOnce(
    new Promise((done, fail) => {
      resolve = done;
      reject = fail;
    }),
  );
  act(() => result.current.setSearch("replacement"));
  let reset!: Promise<unknown>;
  act(() => {
    reset = result.current.refreshObjects({ reset: true }).catch(() => []);
  });
  expect(result.current.nextToken).toBe("");
  const dispatchCount = vi.mocked(apiPost).mock.calls.length;
  await act(async () => oldMore());
  expect(apiPost).toHaveBeenCalledTimes(dispatchCount);
  await act(async () => {
    if (outcome === "rejected") reject(new Error("query unavailable"));
    else {
      const item = response([{ key: "new.txt" }], "new-cursor");
      resolve(outcome === "pending" ? { ...item, status: "approval_pending" } : item);
    }
    await reset;
  });
  await act(async () => oldMore());
  expect(apiPost).toHaveBeenCalledTimes(dispatchCount);
  expect(result.current.nextToken).toBe(outcome === "completed" ? "new-cursor" : "");
  expect(result.current.objects.map((object) => object.key)).toEqual(outcome === "completed" ? ["new.txt"] : ["first.txt"]);
});

it("does not append a retired page into a newer reset result", async () => {
  const { result } = await loadedBrowser();
  let resolveOld!: (_value: unknown) => void;
  vi.mocked(apiPost).mockReturnValueOnce(
    new Promise((done) => {
      resolveOld = done;
    }),
  );
  let oldPage!: Promise<unknown>;
  act(() => {
    oldPage = result.current.refreshObjects({ reset: false, token: "root-cursor" });
  });
  act(() => result.current.setPrefix("new/"));
  vi.mocked(apiPost).mockResolvedValueOnce(response([{ key: "new/only.txt" }], "new-cursor"));
  await act(async () => result.current.refreshObjects({ reset: true }));
  await act(async () => {
    resolveOld(response([{ key: "retired.txt" }], "old-cursor"));
    await oldPage;
  });
  expect(result.current.objects.map((object) => object.key)).toEqual(["new/only.txt"]);
  expect(result.current.nextToken).toBe("new-cursor");
});

it("admits one append at a time and restores its exact query for an explicit retry", async () => {
  const { result } = await loadedBrowser();
  let reject!: (_error: Error) => void;
  vi.mocked(apiPost).mockReturnValueOnce(
    new Promise((_done, fail) => {
      reject = fail;
    }),
  );
  const more = result.current.refreshObjects;
  let append!: Promise<unknown>;
  act(() => {
    append = more({ reset: false, token: "root-cursor" }).catch(() => []);
    void more({ reset: false, token: "root-cursor" });
  });
  expect(apiPost).toHaveBeenCalledTimes(2);
  await act(async () => {
    reject(new Error("page unavailable"));
    await append;
  });
  expect(result.current.nextToken).toBe("root-cursor");
  act(() => result.current.setSearch("unapplied"));
  vi.mocked(apiPost).mockResolvedValueOnce(response([{ key: "second.txt" }], ""));
  await act(async () => result.current.refreshObjects({ reset: false, token: "root-cursor" }));
  expect(connectorActionRequest(vi.mocked(apiPost).mock.lastCall![1]).input).toEqual({
    prefix: "",
    search: "",
    cursor: "root-cursor",
    limit: 100,
  });
  expect(result.current.nextToken).toBe("");
});

it("starts a replacement target at the root instead of using the previous draft", async () => {
  const { result, rerender } = renderHook(({ ref }) => useS3Browser({ target: { ref }, session: { active: true, startedAt: "now" } }), {
    initialProps: { ref: "s3:1:1" },
  });
  await waitFor(() => expect(result.current.nextToken).toBe("root-cursor"));
  act(() => {
    result.current.setPrefix("retired/");
    result.current.setSearch("retired search");
  });
  vi.mocked(apiPost).mockResolvedValueOnce(response([{ key: "new.txt" }], "new-cursor", "s3:2:2"));
  rerender({ ref: "s3:2:2" });
  await waitFor(() => expect(result.current.nextToken).toBe("new-cursor"));
  expect(connectorActionRequest(vi.mocked(apiPost).mock.lastCall![1]).input).toEqual({ prefix: "", search: "", cursor: "", limit: 100 });
  expect(result.current.appliedQuery).toEqual({ prefix: "", search: "" });
});

it("does not dispatch a retained first-page callback after unmount", async () => {
  const { result, unmount } = await loadedBrowser();
  const refresh = result.current.refreshObjects;
  unmount();
  await act(async () => refresh({ reset: true }));
  expect(apiPost).toHaveBeenCalledOnce();
});

it("preserves opaque loaded prefix and search spellings across draft edits", async () => {
  const { result } = await loadedBrowser();
  const query = { prefix: " //odd/../ folder/ ", search: " 字  " };
  act(() => {
    result.current.setPrefix(query.prefix);
    result.current.setSearch(query.search);
  });
  vi.mocked(apiPost).mockResolvedValueOnce(response([{ key: "opaque.txt" }], "opaque-cursor"));
  await act(async () => result.current.refreshObjects({ reset: true }));
  act(() => {
    result.current.setPrefix("draft/");
    result.current.setSearch("draft");
  });
  await act(async () => result.current.refreshObjects({ reset: false, token: "opaque-cursor" }));
  expect(connectorActionRequest(vi.mocked(apiPost).mock.lastCall![1]).input).toEqual({ ...query, cursor: "opaque-cursor", limit: 100 });
});

it.each(["completed", "rejected"])("ignores a retired continuation's %s across session ABA", async (outcome) => {
  const { result, rerender } = renderHook(
    ({ startedAt }) => useS3Browser({ target: { ref: "s3:1:1" }, session: { active: true, startedAt } }),
    { initialProps: { startedAt: "session-a" } },
  );
  await waitFor(() => expect(result.current.nextToken).toBe("root-cursor"));
  let resolve!: (_value: unknown) => void;
  let reject!: (_error: Error) => void;
  vi.mocked(apiPost).mockReturnValueOnce(
    new Promise((done, fail) => {
      resolve = done;
      reject = fail;
    }),
  );
  let old!: Promise<unknown>;
  act(() => {
    old = result.current.refreshObjects({ reset: false, token: "root-cursor" }).catch(() => []);
  });
  const signal = vi.mocked(apiPost).mock.lastCall![2]?.signal;
  vi.mocked(apiPost).mockResolvedValueOnce(response([{ key: "b.txt" }], "b-cursor"));
  rerender({ startedAt: "session-b" });
  await waitFor(() => expect(result.current.nextToken).toBe("b-cursor"));
  vi.mocked(apiPost).mockResolvedValueOnce(response([{ key: "current-a.txt" }], "current-a-cursor"));
  rerender({ startedAt: "session-a" });
  await waitFor(() => expect(result.current.nextToken).toBe("current-a-cursor"));
  expect(signal?.aborted).toBe(true);
  await act(async () => {
    if (outcome === "rejected") reject(new Error("retired continuation"));
    else resolve(response([{ key: "obsolete-a.txt" }], "obsolete-cursor"));
    await old;
  });
  expect(result.current.objects.map((object) => object.key)).toEqual(["current-a.txt"]);
  expect(result.current.nextToken).toBe("current-a-cursor");
  expect(result.current.state.error).toBe("");
});
