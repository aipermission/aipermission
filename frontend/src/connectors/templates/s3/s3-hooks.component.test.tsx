import { act, renderHook, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { useS3ObjectDelete } from "./use-s3-object-delete";
import { useS3Upload } from "./use-s3-upload";

it("passes the selected ETag guard to an approved S3 deletion", async () => {
  const runAction = vi.fn().mockResolvedValue({ output: {} });
  const clearSelection = vi.fn();
  const refreshObjects = vi.fn().mockResolvedValue(null);
  const { result } = renderHook(() =>
    useS3ObjectDelete({
      scopeKey: "target:session",
      selectedKey: "live/file.txt",
      selectedETag: "etag-current",
      trustConditionalRequests: true,
      runAction,
      clearSelection,
      refreshObjects,
    }),
  );
  act(() => result.current.requestDelete());
  expect(result.current.confirmDialog.open).toBe(true);
  await act(async () => result.current.confirmPendingAction());
  expect(runAction).toHaveBeenCalledWith(
    expect.objectContaining({
      actionName: "delete_object",
      input: { key: "live/file.txt", expected_etag: "etag-current" },
    }),
  );
  expect(clearSelection).toHaveBeenCalledOnce();
  expect(refreshObjects).toHaveBeenCalledWith({ reset: true });
  expect(result.current.confirmDialog.open).toBe(false);
});

it("opens S3 uploads in the selected browser prefix", async () => {
  const { result } = renderHook(() =>
    useS3Upload({
      scopeKey: "target:session",
      active: true,
      prefix: "archive/",
      runAction: vi.fn().mockResolvedValue(null),
      refreshObjects: vi.fn().mockResolvedValue(null),
      readObjectMetadata: vi.fn().mockResolvedValue(null),
      setState: vi.fn(),
    }),
  );
  act(() => result.current.openUploadDialog());
  await waitFor(() => expect(result.current.uploadDialog).toMatchObject({ open: true, prefix: "archive/", textKey: "archive/" }));
});
