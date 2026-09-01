import { describe, expect, it, vi } from "vitest";
import { createFileTransferBatchActions, runFileTransferBatchAction } from "./file-transfer-actions";

describe("runFileTransferBatchAction", () => {
  it.each([
    ["pause", {}],
    ["resume", {}],
    ["cancel", {}],
    ["approve", { item_ids: [4, 7], note: "approved subset" }],
    ["decline", { note: "not expected" }],
  ])("posts the %s request and refreshes after success", async (action, body) => {
    const result = { id: 12, status: "running" };
    const post = vi.fn().mockResolvedValue(result);
    const applyResult = vi.fn();
    const refresh = vi.fn().mockResolvedValue(undefined);

    await expect(runFileTransferBatchAction({ action, batchID: 12, body, post, applyResult, refresh })).resolves.toBe(result);

    expect(post).toHaveBeenCalledWith(`/api/file-transfer-batches/12/${action}`, body);
    expect(applyResult).toHaveBeenCalledWith(result);
    expect(refresh).toHaveBeenCalledWith({ keepData: true });
    expect(post.mock.invocationCallOrder[0]).toBeLessThan(refresh.mock.invocationCallOrder[0]);
    expect(applyResult.mock.invocationCallOrder[0]).toBeLessThan(refresh.mock.invocationCallOrder[0]);
  });

  it("does not refresh when the request fails", async () => {
    const post = vi.fn().mockRejectedValue(new Error("approval failed"));
    const applyResult = vi.fn();
    const refresh = vi.fn();

    await expect(
      runFileTransferBatchAction({
        action: "approve",
        batchID: 12,
        body: { item_ids: [4], note: "" },
        post,
        applyResult,
        refresh,
      }),
    ).rejects.toThrow("approval failed");
    expect(applyResult).not.toHaveBeenCalled();
    expect(refresh).not.toHaveBeenCalled();
  });

  it("builds approval and decline payloads through the public action callbacks", async () => {
    const post = vi.fn().mockResolvedValue({});
    const applyResult = vi.fn();
    const refresh = vi.fn().mockResolvedValue(undefined);
    const actions = createFileTransferBatchActions({ post, applyResult, refresh });

    await actions.approve(9, [2, 5], "selected files");
    await actions.decline(10, "unsafe file");

    expect(post).toHaveBeenNthCalledWith(1, "/api/file-transfer-batches/9/approve", {
      item_ids: [2, 5],
      note: "selected files",
    });
    expect(post).toHaveBeenNthCalledWith(2, "/api/file-transfer-batches/10/decline", { note: "unsafe file" });
  });

  it("rejects unknown actions before making a request", async () => {
    const post = vi.fn();
    const applyResult = vi.fn();
    const refresh = vi.fn();

    await expect(runFileTransferBatchAction({ action: "remove", batchID: 12, post, applyResult, refresh })).rejects.toThrow(
      "unsupported file transfer batch action",
    );
    expect(post).not.toHaveBeenCalled();
    expect(applyResult).not.toHaveBeenCalled();
    expect(refresh).not.toHaveBeenCalled();
  });
});
