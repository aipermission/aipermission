import { describe, expect, it } from "vitest";
import { createFileTransferListState, loadCurrentFileTransferBatches } from "./file-transfer-list-state";

describe("file transfer list state", () => {
  it("keeps an authoritative action result when an older list request resolves later", async () => {
    const controller = createFileTransferListState();
    let resolveList;
    const listResponse = new Promise((resolve) => {
      resolveList = resolve;
    });
    let state = { state: "ready", data: [{ id: 7, status: "pending_approval" }], error: null };
    const requestGeneration = controller.beginRequest();
    const applyList = listResponse.then((items) => {
      if (controller.isCurrent(requestGeneration)) state = { state: "ready", data: items, error: null };
    });

    state = controller.applyBatch(state, { id: 7, status: "running" });
    resolveList([{ id: 7, status: "pending_approval" }]);
    await applyList;

    expect(state.data).toEqual([{ id: 7, status: "running" }]);
  });

  it("merges matching batches and prepends newly observed batches", () => {
    const controller = createFileTransferListState();
    let state = { state: "error", data: [{ id: 2, status: "running", direction: "upload" }], error: "old" };
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

  it("drops a stale list response after an authoritative action result", async () => {
    const controller = createFileTransferListState();
    let resolveRequest;
    const request = new Promise((resolve) => {
      resolveRequest = resolve;
    });
    const applied = [];
    const loading = loadCurrentFileTransferBatches({
      request: () => request,
      pollGeneration: 4,
      pollIsCurrent: (generation) => generation === 4,
      listState: controller,
      onItems: (items) => applied.push(items),
      onError: (error) => applied.push(error),
    });

    controller.applyBatch({ state: "ready", data: [], error: null }, { id: 9, status: "completed" });
    resolveRequest({ items: [{ id: 9, status: "running" }] });

    expect(await loading).toEqual([]);
    expect(applied).toEqual([]);
  });

  it("reports only current list request failures", async () => {
    const controller = createFileTransferListState();
    const errors = [];
    await loadCurrentFileTransferBatches({
      request: async () => {
        throw new Error("list failed");
      },
      pollGeneration: 2,
      pollIsCurrent: (generation) => generation === 2,
      listState: controller,
      onItems: () => {},
      onError: (error) => errors.push(error.message),
    });

    expect(errors).toEqual(["list failed"]);
  });
});
