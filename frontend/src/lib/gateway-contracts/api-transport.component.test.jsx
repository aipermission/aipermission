import { afterEach, expect, it, vi } from "vitest";
import { apiDelete, apiDownload, apiGet, downloadJSON, saveBlob } from "../api";

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  delete window.showSaveFilePicker;
});

it("distinguishes a bounded read deadline from caller cancellation", async () => {
  vi.useFakeTimers();
  vi.stubGlobal(
    "fetch",
    vi.fn(
      (_url, { signal }) =>
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
  window.showSaveFilePicker = vi.fn().mockResolvedValue(handle);
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: true, body: { pipeTo }, blob: vi.fn() }));
  await expect(apiDownload("/api/export", "a:b.sql", { requireStreaming: true })).resolves.toEqual({
    saved: true,
    method: "picker",
  });
  expect(window.showSaveFilePicker).toHaveBeenCalledWith({ suggestedName: "a-b.sql" });
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
  window.showSaveFilePicker = vi.fn().mockResolvedValue({ createWritable: vi.fn() });
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: true, body: { cancel } }));

  await expect(apiDownload("/api/export", "backup.aipdb", { requireStreaming: true })).rejects.toThrow("cannot stream this download");
  expect(cancel).toHaveBeenCalledOnce();
});

it("closes response resources when the streaming destination cannot be opened", async () => {
  const cancel = vi.fn().mockResolvedValue(undefined);
  window.showSaveFilePicker = vi.fn().mockResolvedValue({
    createWritable: vi.fn().mockRejectedValue(new Error("destination unavailable")),
  });
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: true, body: { pipeTo: vi.fn(), cancel } }));

  await expect(apiDownload("/api/export", "backup.aipdb", { requireStreaming: true })).rejects.toThrow("destination unavailable");
  expect(cancel).toHaveBeenCalledOnce();
});

it("aborts both streaming resources after a destination write failure", async () => {
  const failure = new Error("stream write failed");
  const abort = vi.fn().mockRejectedValue(new Error("abort already completed"));
  const cancel = vi.fn().mockRejectedValue(new Error("stream already closed"));
  window.showSaveFilePicker = vi.fn().mockResolvedValue({
    createWritable: vi.fn().mockResolvedValue({ abort }),
  });
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
  window.showSaveFilePicker = vi.fn().mockResolvedValue({
    createWritable: vi.fn().mockResolvedValue(writable),
  });
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
  window.showSaveFilePicker = vi
    .fn()
    .mockResolvedValueOnce({ createWritable: async () => writable })
    .mockRejectedValueOnce(new DOMException("Canceled", "AbortError"));
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
