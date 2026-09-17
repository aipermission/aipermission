import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { runGuardedConnectorAction } from "../_shared/action-runner";
import { defaultUploadDialog } from "./dialogs";
import { useS3Browser } from "./use-s3-browser";
import { useS3ObjectDelete } from "./use-s3-object-delete";
import { useS3Upload } from "./use-s3-upload";

vi.mock("../_shared/action-runner", () => ({ runGuardedConnectorAction: vi.fn() }));

const objects = [
  { key: "backups/one.aipdb", size: 10 },
  { key: "backups/two.aipdb", size: 20 },
];

beforeEach(() => {
  runGuardedConnectorAction.mockReset();
  runGuardedConnectorAction.mockImplementation(async ({ actionName, input }) => {
    if (actionName === "list_objects") {
      return { action_name: actionName, output: { directories: [{ prefix: "backups/archive/" }], objects, next_cursor: "next" } };
    }
    if (actionName === "get_object_metadata")
      return { action_name: actionName, output: { key: input.key, content_type: "application/octet-stream" } };
    return { action_name: actionName, output: {} };
  });
});

afterEach(() => {
  delete window.showSaveFilePicker;
  vi.unstubAllGlobals();
});

function renderBrowser() {
  return renderHook(() =>
    useS3Browser({
      target: { ref: "s3:1:1" },
      approvals: { data: [] },
      session: { active: true, startedAt: "now" },
      onRefreshActivity: vi.fn(),
    }),
  );
}

it("loads S3 objects and toggles metadata selection", async () => {
  const { result } = renderBrowser();
  await waitFor(() => expect(result.current.objects).toHaveLength(2));
  expect(result.current.visibleBytes).toBe(30);

  await act(async () => result.current.selectObject(objects[0].key));
  expect(result.current.selectedKey).toBe(objects[0].key);
  expect(result.current.metadata).toEqual(expect.objectContaining({ key: objects[0].key }));

  await act(async () => result.current.selectObject(objects[0].key));
  expect(result.current.selectedKey).toBe("");
  expect(result.current.metadata).toBeNull();
});

it("cancels an S3 download before dispatch when the file picker is dismissed", async () => {
  window.showSaveFilePicker = vi.fn().mockRejectedValue(new DOMException("Canceled", "AbortError"));
  const { result } = renderBrowser();
  await waitFor(() => expect(result.current.objects).toHaveLength(2));
  await act(async () => result.current.selectObject(objects[0].key));
  runGuardedConnectorAction.mockClear();

  await act(async () => result.current.downloadSelected());

  expect(result.current.state.message).toBe("Download canceled.");
  expect(runGuardedConnectorAction).not.toHaveBeenCalled();
});

it("validates S3 uploads before dispatch and refreshes successful text objects", async () => {
  const runAction = vi.fn().mockResolvedValue({ action_name: "upload_object" });
  const refreshObjects = vi.fn().mockResolvedValue([]);
  const readObjectMetadata = vi.fn().mockResolvedValue(undefined);
  const setState = vi.fn();
  const { result } = renderHook(() =>
    useS3Upload({ scopeKey: "s3:1:1:now", active: true, prefix: "notes/", runAction, refreshObjects, readObjectMetadata, setState }),
  );

  act(() => result.current.openUploadDialog());
  await act(async () => result.current.uploadObjects({ preventDefault: vi.fn() }));
  expect(result.current.uploadDialog.error).toBe("Choose one or more files to upload.");
  expect(runAction).not.toHaveBeenCalled();

  act(() =>
    result.current.setUploadDialog({
      ...defaultUploadDialog,
      open: true,
      mode: "text",
      textKey: "notes/readme.txt",
      textContent: "hello",
      textContentType: "text/plain",
    }),
  );
  await act(async () => result.current.uploadObjects({ preventDefault: vi.fn() }));
  expect(runAction).toHaveBeenCalledWith(
    expect.objectContaining({
      actionName: "upload_object",
      input: expect.objectContaining({ key: "notes/readme.txt", content_text: "hello", overwrite: false }),
    }),
  );
  expect(refreshObjects).toHaveBeenCalledWith({ reset: true });
  expect(readObjectMetadata).toHaveBeenCalledWith("notes/readme.txt");
  expect(result.current.uploadDialog.open).toBe(false);
});

it("aborts file preparation before an old S3 target can dispatch an upload", async () => {
  class PendingFileReader {
    static EMPTY = 0;
    static LOADING = 1;
    static instances = [];

    constructor() {
      this.readyState = PendingFileReader.EMPTY;
      PendingFileReader.instances.push(this);
    }

    readAsDataURL() {
      this.readyState = PendingFileReader.LOADING;
    }

    abort() {
      this.readyState = PendingFileReader.EMPTY;
      this.onabort?.();
    }
  }
  vi.stubGlobal("FileReader", PendingFileReader);
  const runAction = vi.fn();
  const props = {
    active: true,
    prefix: "",
    runAction,
    refreshObjects: vi.fn(),
    readObjectMetadata: vi.fn(),
    setState: vi.fn(),
  };
  const { result, rerender } = renderHook(({ scopeKey }) => useS3Upload({ ...props, scopeKey }), {
    initialProps: { scopeKey: "s3:1:1:now" },
  });
  const file = new File(["old"], "old.txt", { type: "text/plain" });
  act(() => {
    result.current.setUploadDialog({
      ...defaultUploadDialog,
      open: true,
      files: [{ id: "old", file, key: "old.txt", contentType: "text/plain" }],
    });
  });

  let upload;
  act(() => {
    upload = result.current.uploadObjects({ preventDefault: vi.fn() });
  });
  expect(PendingFileReader.instances).toHaveLength(1);
  rerender({ scopeKey: "s3:2:2:now" });
  await act(async () => upload);

  expect(runAction).not.toHaveBeenCalled();
  expect(result.current.uploadDialog).toEqual(defaultUploadDialog);
});

it("keeps metadata empty when selection is cleared before detail completes", async () => {
  let resolveMetadata;
  const metadata = new Promise((resolve) => {
    resolveMetadata = resolve;
  });
  runGuardedConnectorAction.mockImplementation(async ({ actionName, input, requestGuard, channel }) => {
    if (actionName === "list_objects") return { action_name: actionName, output: { objects, directories: [] } };
    const request = requestGuard.begin(channel);
    const item = await metadata;
    const result = request.isCurrent() ? { action_name: actionName, output: { key: input.key, ...item } } : null;
    request.complete();
    return result;
  });
  const { result } = renderBrowser();
  await waitFor(() => expect(result.current.objects).toHaveLength(2));

  let selection;
  act(() => {
    selection = result.current.selectObject(objects[0].key);
  });
  act(() => result.current.clearSelection());
  await act(async () => resolveMetadata({ content_type: "text/plain" }));
  await selection;

  expect(result.current.selectedKey).toBe("");
  expect(result.current.metadata).toBeNull();
});

it("binds destructive S3 confirmation to the requested object", async () => {
  const runAction = vi.fn().mockResolvedValue({ action_name: "delete_object" });
  const clearSelection = vi.fn();
  const refreshObjects = vi.fn().mockResolvedValue([]);
  const { result, rerender } = renderHook(
    ({ selectedKey, selectedETag }) =>
      useS3ObjectDelete({
        scopeKey: "s3:1:1:now",
        selectedKey,
        selectedETag,
        trustConditionalRequests: true,
        runAction,
        clearSelection,
        refreshObjects,
      }),
    { initialProps: { selectedKey: objects[0].key, selectedETag: '"etag-one"' } },
  );

  act(() => result.current.requestDelete());
  rerender({ selectedKey: objects[1].key, selectedETag: "etag-two" });
  await act(async () => result.current.confirmPendingAction());

  expect(runAction).toHaveBeenCalledWith(
    expect.objectContaining({ actionName: "delete_object", input: { key: objects[0].key, expected_etag: '"etag-one"' }, busy: "deleting" }),
  );
  expect(clearSelection).toHaveBeenCalledOnce();
  expect(refreshObjects).toHaveBeenCalledWith({ reset: true });
});

it("omits conditional deletion guards for default S3 targets", async () => {
  const runAction = vi.fn().mockResolvedValue({ action_name: "delete_object" });
  const { result } = renderHook(() =>
    useS3ObjectDelete({
      scopeKey: "s3:1:1:now",
      selectedKey: objects[0].key,
      selectedETag: '"etag-one"',
      runAction,
      clearSelection: vi.fn(),
      refreshObjects: vi.fn().mockResolvedValue([]),
    }),
  );

  act(() => result.current.requestDelete());
  await act(async () => result.current.confirmPendingAction());

  expect(runAction).toHaveBeenCalledWith(
    expect.objectContaining({ actionName: "delete_object", input: { key: objects[0].key }, busy: "deleting" }),
  );
});

it("omits an empty ETag when conditional S3 requests are trusted", async () => {
  const runAction = vi.fn().mockResolvedValue({ action_name: "delete_object" });
  const { result } = renderHook(() =>
    useS3ObjectDelete({
      scopeKey: "s3:1:1:now",
      selectedKey: objects[0].key,
      trustConditionalRequests: true,
      runAction,
      clearSelection: vi.fn(),
      refreshObjects: vi.fn().mockResolvedValue([]),
    }),
  );

  act(() => result.current.requestDelete());
  await act(async () => result.current.confirmPendingAction());
  expect(runAction).toHaveBeenCalledWith(expect.objectContaining({ input: { key: objects[0].key } }));
});

it("retains pending approval feedback and action failures inside the S3 confirmation", async () => {
  const runAction = vi.fn().mockResolvedValueOnce(null).mockRejectedValueOnce(new Error("bucket denied"));
  const { result } = renderHook(() =>
    useS3ObjectDelete({
      scopeKey: "s3:1:1:now",
      selectedKey: objects[0].key,
      runAction,
      clearSelection: vi.fn(),
      refreshObjects: vi.fn(),
    }),
  );
  act(() => result.current.requestDelete());

  await act(async () => result.current.confirmPendingAction());
  expect(result.current.confirmDialog).toMatchObject({ open: true, pending: false, status: expect.stringContaining("pending") });
  await act(async () => result.current.confirmPendingAction());
  expect(result.current.confirmDialog).toMatchObject({ open: true, pending: false, error: "bucket denied" });
});

it("does not dismiss a destructive confirmation during a running S3 request", async () => {
  let resolveAction;
  const runAction = vi.fn().mockReturnValue(
    new Promise((resolve) => {
      resolveAction = resolve;
    }),
  );
  const { result } = renderHook(() =>
    useS3ObjectDelete({
      scopeKey: "s3:1:1:now",
      selectedKey: objects[0].key,
      runAction,
      clearSelection: vi.fn(),
      refreshObjects: vi.fn(),
    }),
  );
  act(() => result.current.requestDelete());
  let request;
  act(() => {
    request = result.current.confirmPendingAction();
  });
  act(() => result.current.closeConfirmDialog());
  expect(result.current.confirmDialog).toMatchObject({ open: true, pending: true });
  await act(async () => resolveAction(null));
  await request;
  expect(result.current.confirmDialog.open).toBe(true);
});
