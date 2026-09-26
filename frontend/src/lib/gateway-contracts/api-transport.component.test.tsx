import { afterEach, expect, it, vi } from "vitest";
import { apiDelete, apiDownload, apiGet, downloadJSON, saveBlob } from "../api";
import type { NativeFileWriter } from "../api-types";

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  Reflect.deleteProperty(window, "showSaveFilePicker");
});

it("distinguishes a bounded read deadline from caller cancellation", async () => {
  vi.useFakeTimers();
  vi.stubGlobal(
    "fetch",
    vi.fn(
      (_url: unknown, { signal }: { signal: AbortSignal }) =>
        new Promise((_resolve, reject) =>
          signal.addEventListener("abort", () => reject(signal.reason || new DOMException("Aborted", "AbortError")), { once: true }),
        ),
    ),
  );
  const timed = apiGet("/api/status", { timeoutMs: 25 });
  const timedExpectation = expect(timed).rejects.toThrow("Gateway read timed out after 25ms");
  await vi.advanceTimersByTimeAsync(25);
  await timedExpectation;

  const controller = new AbortController();
  const canceled = apiGet("/api/status", { timeoutMs: 50, signal: controller.signal });
  controller.abort(new Error("caller canceled"));
  await expect(canceled).rejects.toThrow("caller canceled");
  await vi.runAllTimersAsync();
});

it("preserves cancellation that happened before a bounded read starts and clears its deadline", async () => {
  vi.useFakeTimers();
  const parent = new AbortController();
  const failure = new Error("already canceled");
  parent.abort(failure);
  const fetch = vi.fn<typeof globalThis.fetch>().mockImplementation(async (_url, options) => {
    const signal = options?.signal;
    if (!signal) throw new Error("Missing bounded read signal");
    expect(signal.aborted).toBe(true);
    expect(signal.reason).toBe(failure);
    signal.throwIfAborted();
    return new Response("{}");
  });
  vi.stubGlobal("fetch", fetch);
  await expect(apiGet("/api/status", { signal: parent.signal, timeoutMs: 50 })).rejects.toBe(failure);
  expect(fetch).toHaveBeenCalledOnce();
  expect(vi.getTimerCount()).toBe(0);
});

it.each(["success", "network failure"])("removes bounded-read listeners and timers after %s", async (outcome) => {
  vi.useFakeTimers();
  const parent = new AbortController();
  const remove = vi.spyOn(parent.signal, "removeEventListener");
  const failure = new Error("network unavailable");
  let requestSignal: AbortSignal | null = null;
  vi.stubGlobal(
    "fetch",
    vi.fn<typeof fetch>().mockImplementation(async (_url, options) => {
      requestSignal = options?.signal || null;
      if (outcome === "network failure") throw failure;
      return new Response('{"ok":true}');
    }),
  );
  const read = apiGet("/api/status", { signal: parent.signal, timeoutMs: 50 });
  if (outcome === "success") await expect(read).resolves.toEqual({ ok: true });
  else await expect(read).rejects.toBe(failure);
  expect(remove).toHaveBeenCalledWith("abort", expect.any(Function));
  expect(vi.getTimerCount()).toBe(0);
  parent.abort(new Error("canceled after completion"));
  await vi.advanceTimersByTimeAsync(100);
  expect(requestSignal).not.toBeNull();
  expect(requestSignal).toHaveProperty("aborted", false);
});

it.each(["open", "write", "close"])("preserves the original buffered-picker %s failure and aborts an opened writer", async (step) => {
  const failure = new Error(`destination ${step} failed`);
  const write = vi.fn<NativeFileWriter["write"]>().mockResolvedValue(undefined);
  const close = vi.fn<NativeFileWriter["close"]>().mockResolvedValue(undefined);
  const abort = vi.fn<NativeFileWriter["abort"]>().mockRejectedValue(new Error("cleanup also failed"));
  const writer = Object.assign(new WritableStream<Uint8Array>(), { write, close, abort }) satisfies NativeFileWriter;
  if (step === "write") write.mockRejectedValueOnce(failure);
  if (step === "close") close.mockRejectedValueOnce(failure);
  const createWritable = vi.fn().mockImplementation(async () => {
    if (step === "open") throw failure;
    return writer;
  });
  Reflect.set(window, "showSaveFilePicker", vi.fn().mockResolvedValue({ createWritable }));
  const response = new Response("small export", { headers: { "Content-Type": "text/plain" } });
  if (!response.body) throw new Error("Missing response body");
  Object.defineProperty(response.body, "pipeTo", { value: undefined });
  const fetch = vi.fn<typeof globalThis.fetch>().mockResolvedValue(response);
  vi.stubGlobal("fetch", fetch);
  await expect(apiDownload("/api/export", "small.txt", { picker: true })).rejects.toBe(failure);
  expect(fetch).toHaveBeenCalledOnce();
  expect(createWritable).toHaveBeenCalledOnce();
  if (step === "open") {
    expect(write).not.toHaveBeenCalled();
    expect(abort).not.toHaveBeenCalled();
  } else {
    expect(write).toHaveBeenCalledExactlyOnceWith(expect.any(Blob));
    expect(abort).toHaveBeenCalledExactlyOnceWith(failure);
  }
  expect(close).toHaveBeenCalledTimes(step === "close" ? 1 : 0);
});

it.each(["download", "blob"])("does not fall back after a %s picker permission failure", async (kind) => {
  const failure = new Error("picker permission denied");
  Reflect.set(window, "showSaveFilePicker", vi.fn().mockRejectedValue(failure));
  const fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);
  const anchor = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});
  const download =
    kind === "download"
      ? apiDownload("/api/export", "data.txt", { picker: true })
      : saveBlob(new Blob(["data"]), "data.txt", { picker: true });
  await expect(download).rejects.toBe(failure);
  expect(fetch).not.toHaveBeenCalled();
  expect(anchor).not.toHaveBeenCalled();
});

it("supports empty and JSON DELETE responses", async () => {
  const fetch = vi
    .fn()
    .mockResolvedValueOnce(new Response(null, { status: 204 }))
    .mockResolvedValueOnce(new Response('{"deleted":true}', { status: 200 }));
  vi.stubGlobal("fetch", fetch);
  await expect(apiDelete("/api/items/1")).resolves.toBeNull();
  await expect(apiDelete("/api/items/2")).resolves.toEqual({ deleted: true });
  expect(fetch.mock.calls[0][1]).toMatchObject({ method: "DELETE", credentials: "include" });
});

it("streams required downloads to a native picker when the response supports piping", async () => {
  const pipeTo = vi.fn().mockResolvedValue(undefined);
  const writable = { write: vi.fn(), close: vi.fn() };
  const handle = { createWritable: vi.fn().mockResolvedValue(writable) };
  Reflect.set(window, "showSaveFilePicker", vi.fn().mockResolvedValue(handle));
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: true, body: { pipeTo }, blob: vi.fn() }));
  await expect(apiDownload("/api/export", "a:b.sql", { requireStreaming: true })).resolves.toEqual({
    saved: true,
    method: "picker",
  });
  expect(Reflect.get(window, "showSaveFilePicker")).toHaveBeenCalledWith({ suggestedName: "a-b.sql" });
  expect(pipeTo).toHaveBeenCalledWith(writable, { signal: undefined });
});

it("rejects required streaming before dispatch when the native picker is unavailable", async () => {
  const fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);

  await expect(apiDownload("/api/export", "backup.aipdb", { requireStreaming: true })).rejects.toThrow(
    "requires a browser with a streaming Save dialog",
  );
  expect(fetch).not.toHaveBeenCalled();
});

it("keeps the streaming compatibility error when response cancellation fails", async () => {
  const cancel = vi.fn().mockRejectedValue(new Error("cancel failed"));
  Reflect.set(window, "showSaveFilePicker", vi.fn().mockResolvedValue({ createWritable: vi.fn() }));
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: true, body: { cancel } }));

  await expect(apiDownload("/api/export", "backup.aipdb", { requireStreaming: true })).rejects.toThrow("cannot stream this download");
  expect(cancel).toHaveBeenCalledOnce();
});

it("closes response resources when the streaming destination cannot be opened", async () => {
  const cancel = vi.fn().mockResolvedValue(undefined);
  Reflect.set(
    window,
    "showSaveFilePicker",
    vi.fn().mockResolvedValue({
      createWritable: vi.fn().mockRejectedValue(new Error("destination unavailable")),
    }),
  );
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: true, body: { pipeTo: vi.fn(), cancel } }));

  await expect(apiDownload("/api/export", "backup.aipdb", { requireStreaming: true })).rejects.toThrow("destination unavailable");
  expect(cancel).toHaveBeenCalledOnce();
});

it("aborts both streaming resources after a destination write failure", async () => {
  const failure = new Error("stream write failed");
  const abort = vi.fn().mockRejectedValue(new Error("abort already completed"));
  const cancel = vi.fn().mockRejectedValue(new Error("stream already closed"));
  Reflect.set(
    window,
    "showSaveFilePicker",
    vi.fn().mockResolvedValue({
      createWritable: vi.fn().mockResolvedValue({ abort }),
    }),
  );
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue({
      ok: true,
      body: { pipeTo: vi.fn().mockRejectedValue(failure), cancel },
    }),
  );

  await expect(apiDownload("/api/export", "backup.aipdb", { requireStreaming: true })).rejects.toThrow("stream write failed");
  expect(abort).toHaveBeenCalledWith(failure);
  expect(cancel).toHaveBeenCalledWith(failure);
});

it("buffers an ordinary picker download when direct stream piping is unavailable", async () => {
  const payload = new TextEncoder().encode("small export");
  const read = vi.fn().mockResolvedValueOnce({ done: false, value: payload }).mockResolvedValueOnce({ done: true });
  const releaseLock = vi.fn();
  const writable = { write: vi.fn(), close: vi.fn() };
  Reflect.set(
    window,
    "showSaveFilePicker",
    vi.fn().mockResolvedValue({
      createWritable: vi.fn().mockResolvedValue(writable),
    }),
  );
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue({
      ok: true,
      headers: new Headers({ "Content-Type": "text/plain" }),
      body: { getReader: () => ({ read, releaseLock }) },
    }),
  );

  await expect(apiDownload("/api/export", "small.txt", { picker: true })).resolves.toEqual({ saved: true, method: "picker" });
  expect(writable.write).toHaveBeenCalledWith(expect.any(Blob));
  expect(writable.close).toHaveBeenCalledOnce();
  expect(releaseLock).toHaveBeenCalledOnce();
});

it("writes picker blobs, handles cancellation, and creates JSON downloads", async () => {
  const writable = { write: vi.fn(), close: vi.fn() };
  Reflect.set(
    window,
    "showSaveFilePicker",
    vi
      .fn()
      .mockResolvedValueOnce({ createWritable: async () => writable })
      .mockRejectedValueOnce(new DOMException("Canceled", "AbortError")),
  );
  const blob = new Blob(["payload"]);
  await expect(saveBlob(blob, "a:b.txt", { picker: true })).resolves.toEqual({ saved: true, method: "picker" });
  expect(writable.write).toHaveBeenCalledWith(blob);
  await expect(saveBlob(blob, "cancel.txt", { picker: true })).resolves.toEqual({ saved: false, canceled: true, method: "picker" });

  const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});
  vi.stubGlobal("URL", { createObjectURL: vi.fn(() => "blob:test"), revokeObjectURL: vi.fn() });
  downloadJSON({ ok: true }, "result:1.json");
  expect(click).toHaveBeenCalledOnce();
  expect(URL.revokeObjectURL).toHaveBeenCalledWith("blob:test");
});
