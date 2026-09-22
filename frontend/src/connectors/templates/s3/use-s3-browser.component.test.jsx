import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { saveBlob } from "../../../lib/api";
import { runGuardedConnectorAction } from "../_shared/action-runner";
import { defaultUploadDialog } from "./dialogs";
import { useS3Browser } from "./use-s3-browser";
import { useS3ObjectDelete } from "./use-s3-object-delete";
import { useS3Upload } from "./use-s3-upload";

vi.mock("../_shared/action-runner", () => ({ runGuardedConnectorAction: vi.fn() }));
vi.mock("../../../lib/api", () => ({ saveBlob: vi.fn() }));

const objects = [
  { key: "backups/one.aipdb", size: 10 },
  { key: "backups/two.aipdb", size: 20 },
];

beforeEach(() => {
  saveBlob.mockReset().mockResolvedValue(undefined);
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
  return renderHook(
    ({ target }) =>
      useS3Browser({
        target,
        approvals: { data: [] },
        session: { active: true, startedAt: "now" },
        onRefreshActivity: vi.fn(),
      }),
    {
      initialProps: { target: { ref: "s3:1:1" } },
    },
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

it("aborts a failed native S3 download writer and reports the error", async () => {
  const abort = vi.fn().mockResolvedValue(undefined);
  const write = vi.fn().mockRejectedValue(new Error("disk full"));
  window.showSaveFilePicker = vi.fn().mockResolvedValue({
    createWritable: vi.fn().mockResolvedValue({ write, close: vi.fn(), abort }),
  });
  runGuardedConnectorAction.mockImplementation(async ({ actionName, input }) => {
    if (actionName === "list_objects") return { action_name: actionName, output: { objects, directories: [] } };
    if (actionName === "get_object_metadata") return { action_name: actionName, output: { key: input.key } };
    if (actionName === "download_object") {
      return { action_name: actionName, output: { filename: "one.aipdb", content_base64: "aGVsbG8=" } };
    }
    return null;
  });
  const { result } = renderBrowser();
  await waitFor(() => expect(result.current.objects).toHaveLength(2));
  await act(async () => result.current.selectObject(objects[0].key));

  await act(async () => result.current.downloadSelected());

  expect(write).toHaveBeenCalledOnce();
  expect(abort).toHaveBeenCalledOnce();
  expect(result.current.state).toEqual({ state: "error", error: "disk full", message: "" });
});

it("does not dispatch a download after the target changes while the picker is open", async () => {
  let resolvePicker;
  window.showSaveFilePicker = vi.fn().mockReturnValue(
    new Promise((resolve) => {
      resolvePicker = resolve;
    }),
  );
  const { result, rerender } = renderBrowser();
  await waitFor(() => expect(result.current.objects).toHaveLength(2));
  await act(async () => result.current.selectObject(objects[0].key));
  runGuardedConnectorAction.mockClear();

  let download;
  act(() => {
    download = result.current.downloadSelected();
  });
  rerender({ target: { ref: "s3:2:2" } });
  await act(async () => resolvePicker({ createWritable: vi.fn() }));
  await download;

  expect(runGuardedConnectorAction.mock.calls.some(([options]) => options.actionName === "download_object")).toBe(false);
});

it("reports picker and buffered download failures", async () => {
  window.showSaveFilePicker = vi.fn().mockRejectedValue(new Error("picker unavailable"));
  const { result } = renderBrowser();
  await waitFor(() => expect(result.current.objects).toHaveLength(2));
  await act(async () => result.current.selectObject(objects[0].key));

  await act(async () => result.current.downloadSelected());
  expect(result.current.state).toEqual({ state: "error", error: "picker unavailable", message: "" });

  delete window.showSaveFilePicker;
  runGuardedConnectorAction.mockImplementation(async ({ actionName, input }) => {
    if (actionName === "list_objects") return { action_name: actionName, output: { objects, directories: [] } };
    if (actionName === "get_object_metadata") return { action_name: actionName, output: { key: input.key } };
    if (actionName === "download_object") {
      return { action_name: actionName, output: { filename: "one.aipdb", content_base64: "aGVsbG8=" } };
    }
    return null;
  });
  saveBlob.mockRejectedValueOnce(new Error("local save failed"));
  await act(async () => result.current.downloadSelected());

  expect(result.current.state).toEqual({ state: "error", error: "local save failed", message: "" });
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

it("rejects oversized S3 files before reading or dispatching them", async () => {
  const runAction = vi.fn();
  const { result } = renderHook(() =>
    useS3Upload({
      scopeKey: "s3:1:1:now",
      active: true,
      prefix: "",
      runAction,
      refreshObjects: vi.fn(),
      readObjectMetadata: vi.fn(),
      setState: vi.fn(),
    }),
  );
  act(() => {
    result.current.openUploadDialog();
    result.current.addUploadFiles([{ name: "large.bin", size: (16 << 20) + 1, lastModified: 1, type: "application/octet-stream" }]);
  });

  await act(async () => result.current.uploadObjects({ preventDefault: vi.fn() }));

  expect(result.current.uploadDialog.error).toBe("Choose files no larger than 16 MiB.");
  expect(runAction).not.toHaveBeenCalled();
});

it("updates, removes, and closes staged S3 upload files", () => {
  const { result } = renderHook(() =>
    useS3Upload({
      scopeKey: "s3:1:1:now",
      active: true,
      prefix: "",
      runAction: vi.fn(),
      refreshObjects: vi.fn(),
      readObjectMetadata: vi.fn(),
      setState: vi.fn(),
    }),
  );
  act(() => {
    result.current.openUploadDialog();
    result.current.addUploadFiles([new File(["a"], "a.txt")]);
  });
  const id = result.current.uploadDialog.files[0].id;

  act(() => result.current.updateUploadFile(id, { key: "renamed.txt" }));
  expect(result.current.uploadDialog.files[0].key).toBe("renamed.txt");
  act(() => result.current.removeUploadFile(id));
  expect(result.current.uploadDialog.files).toEqual([]);
  act(() => result.current.closeUploadDialog());
  expect(result.current.uploadDialog).toEqual(defaultUploadDialog);
});

it.each([false, true])("retries only unfinished S3 files after a partial upload when overwrite is %s", async (overwrite) => {
  const runAction = vi
    .fn()
    .mockResolvedValueOnce({ action_name: "upload_object" })
    .mockRejectedValueOnce(new Error("temporary failure"))
    .mockResolvedValueOnce({ action_name: "upload_object" });
  const refreshObjects = vi.fn().mockResolvedValue([]);
  const { result } = renderHook(() =>
    useS3Upload({
      scopeKey: "s3:1:1:now",
      active: true,
      prefix: "",
      runAction,
      refreshObjects,
      readObjectMetadata: vi.fn(),
      setState: vi.fn(),
    }),
  );
  act(() => {
    result.current.openUploadDialog();
    result.current.addUploadFiles([new File(["a"], "a.txt"), new File(["b"], "b.txt")]);
  });
  if (overwrite) act(() => result.current.setUploadDialog((current) => ({ ...current, overwrite: true })));

  await act(async () => result.current.uploadObjects({ preventDefault: vi.fn() }));
  expect(result.current.uploadDialog).toMatchObject({ pending: false, error: "temporary failure" });
  expect(result.current.uploadDialog.files.map((item) => item.key)).toEqual(["b.txt"]);

  await act(async () => result.current.uploadObjects({ preventDefault: vi.fn() }));
  expect(runAction.mock.calls.map(([options]) => options.input.key)).toEqual(["a.txt", "b.txt", "b.txt"]);
  expect(runAction.mock.calls.every(([options]) => options.input.overwrite === overwrite)).toBe(true);
  expect(refreshObjects).toHaveBeenCalledWith({ reset: true });
  expect(result.current.uploadDialog).toEqual(defaultUploadDialog);
});

it("retains an S3 file whose upload is pending approval", async () => {
  const runAction = vi.fn().mockResolvedValueOnce({ action_name: "upload_object" }).mockResolvedValueOnce(null);
  const { result } = renderHook(() =>
    useS3Upload({
      scopeKey: "s3:1:1:now",
      active: true,
      prefix: "",
      runAction,
      refreshObjects: vi.fn(),
      readObjectMetadata: vi.fn(),
      setState: vi.fn(),
    }),
  );
  act(() => {
    result.current.openUploadDialog();
    result.current.addUploadFiles([new File(["a"], "a.txt"), new File(["b"], "b.txt")]);
  });

  await act(async () => result.current.uploadObjects({ preventDefault: vi.fn() }));
  expect(result.current.uploadDialog).toMatchObject({ open: true, pending: false, error: "" });
  expect(result.current.uploadDialog.files.map((item) => item.key)).toEqual(["b.txt"]);
});

it("retains the uncertain S3 file after an upload error", async () => {
  const runAction = vi.fn().mockResolvedValueOnce({ action_name: "upload_object" }).mockRejectedValueOnce(new Error("connection lost"));
  const { result } = renderHook(() =>
    useS3Upload({
      scopeKey: "s3:1:1:now",
      active: true,
      prefix: "",
      runAction,
      refreshObjects: vi.fn(),
      readObjectMetadata: vi.fn(),
      setState: vi.fn(),
    }),
  );
  act(() => {
    result.current.openUploadDialog();
    result.current.addUploadFiles([new File(["a"], "a.txt"), new File(["b"], "b.txt")]);
  });

  await act(async () => result.current.uploadObjects({ preventDefault: vi.fn() }));
  expect(result.current.uploadDialog).toMatchObject({ open: true, pending: false, error: "connection lost" });
  expect(result.current.uploadDialog.files.map((item) => item.key)).toEqual(["b.txt"]);
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
