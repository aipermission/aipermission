import { describe, expect, it, vi } from "vitest";
import { createFileTransferListState, fileTransferListBatchResponse, loadCurrentFileTransferBatches } from "./file-transfer-list-state";
import type { FileTransferListBatch, FileTransferListState } from "./file-transfer-list-state";
import { errorMessage } from "../../lib/errors";

describe("file transfer list state", () => {
  it.each([
    { direction: {} }, { source: false }, { error: [] }, { transferred_bytes: "10" }, { eta_seconds: Infinity },
    { runtime_id: {} }, { items: {} }, { items: [{ id: 1, status: "pending", remote_path: false }] },
    { items: [{ id: 1, status: "pending", size_bytes: "1" }] }, { items: [{ id: 0, status: "pending" }] },
  ])("rejects malformed transfer presentation data: %j", (fields) => {
    expect(() => fileTransferListBatchResponse({ id: 7, status: "running", ...fields })).toThrow("Invalid transfer list batch");
  });

  it("preserves validated presentation fields and opaque extension data", () => {
    const batch = {
      id: 7, status: "running", direction: "upload", source: "ui", runtime_id: "runtime:7", transferred_bytes: 10,
      eta_seconds: -1, items: [{ id: 9, status: "running", file_name: "test.txt", size_bytes: 100 }], extension: { future: true },
    };
    expect(fileTransferListBatchResponse(batch)).toEqual(batch);
  });

  it("keeps an authoritative action result when an older list request resolves later", async () => {
    const controller = createFileTransferListState();
    let resolveList: ((_items: FileTransferListBatch[]) => void) | undefined;
    const listResponse = new Promise<FileTransferListBatch[]>((resolve) => {
      resolveList = resolve;
    });
    let state: FileTransferListState = { state: "ready", data: [{ id: 7, status: "pending_approval" }], error: null };
    const requestGeneration = controller.beginRequest();
    const applyList = listResponse.then((items) => {
      if (controller.isCurrent(requestGeneration)) state = { state: "ready", data: items, error: null };
    });

    state = controller.applyBatch(state, { id: 7, status: "running" });
    resolveList?.([{ id: 7, status: "pending_approval" }]);
    await applyList;

    expect(state.data).toEqual([{ id: 7, status: "running" }]);
  });

  it("merges matching batches and prepends newly observed batches", () => {
    const controller = createFileTransferListState();
    let state: FileTransferListState = { state: "error", data: [{ id: 2, status: "running", direction: "upload" }], error: "old" };
    state = controller.applyBatch(state, { id: 2, status: "completed" });
    state = controller.applyBatch(state, { id: 3, status: "pending_approval" });
    expect(state).toEqual({
      state: "ready",
      data: [
        { id: 3, status: "pending_approval" },
        { id: 2, status: "completed", direction: "upload" },
      ],
      error: null,
    });
  });

  it("does not regress a terminal batch with an older active action response", () => {
    const controller = createFileTransferListState();
    const terminal = { id: 4, status: "completed", completed_items: 2, total_items: 2 };
    const state = controller.applyBatch(
      { state: "ready", data: [terminal], error: null },
      { id: 4, status: "running", completed_items: 1 },
    );

    expect(state.data).toEqual([terminal]);
  });

  it("drops a stale list response after an authoritative action result", async () => {
    const controller = createFileTransferListState();
    let resolveRequest: ((_value: { items: FileTransferListBatch[] }) => void) | undefined;
    const request = new Promise<{ items: FileTransferListBatch[] }>((resolve) => {
      resolveRequest = resolve;
    });
    const applied: unknown[] = [];
    const loading = loadCurrentFileTransferBatches({
      request: () => request,
      pollGeneration: 4,
      pollIsCurrent: (generation) => generation === 4,
      listState: controller,
      onItems: (items) => applied.push(items),
      onError: (error) => applied.push(error),
    });

    controller.applyBatch({ state: "ready", data: [], error: null }, { id: 9, status: "completed" });
    resolveRequest?.({ items: [{ id: 9, status: "running" }] });

    expect(await loading).toEqual([]);
    expect(applied).toEqual([]);
  });

  it("reports only current list request failures", async () => {
    const controller = createFileTransferListState();
    const errors: string[] = [];
    await loadCurrentFileTransferBatches({
      request: async () => {
        throw new Error("list failed");
      },
      pollGeneration: 2,
      pollIsCurrent: (generation) => generation === 2,
      listState: controller,
      onItems: () => {},
      onError: (error) => errors.push(errorMessage(error)),
    });

    expect(errors).toEqual(["list failed"]);
  });

  it("accepts newer terminal feedback while retaining opaque batch fields", () => {
    const controller = createFileTransferListState();
    const current = {
      state: "ready",
      data: [{ id: 4, status: "failed", failure_kind: "timeout", extension: { future: true } }],
      error: null,
    };
    const next = controller.applyBatch(current, { id: 4, status: "completed", completed_items: 2 });
    expect(next.data[0]).toEqual({ id: 4, status: "completed", failure_kind: "timeout", extension: { future: true }, completed_items: 2 });
    expect(current.data[0].status).toBe("failed");
  });

  it.each([undefined, null, {}])("publishes an empty current list for an absent items envelope", async (response) => {
    const onItems = vi.fn();
    const onError = vi.fn();
    expect(
      await loadCurrentFileTransferBatches({
        request: async () => response,
        pollGeneration: 1,
        pollIsCurrent: () => true,
        listState: createFileTransferListState(),
        onItems,
        onError,
      }),
    ).toEqual([]);
    expect(onItems).toHaveBeenCalledWith([]);
    expect(onError).not.toHaveBeenCalled();
  });

  it.each(["poll", "list"])("drops errors owned by a stale %s generation", async (owner) => {
    const listState = createFileTransferListState();
    let reject: ((_error: Error) => void) | undefined;
    const request = new Promise<never>((_resolve, rejectPromise) => {
      reject = rejectPromise;
    });
    const onError = vi.fn();
    const onItems = vi.fn();
    const loading = loadCurrentFileTransferBatches({
      request: () => request,
      pollGeneration: 1,
      pollIsCurrent: () => owner !== "poll",
      listState,
      onItems,
      onError,
    });
    if (owner === "list") listState.beginRequest();
    reject?.(new Error("stale failure"));
    expect(await loading).toEqual([]);
    expect(onError).not.toHaveBeenCalled();
    expect(onItems).not.toHaveBeenCalled();
  });
});
