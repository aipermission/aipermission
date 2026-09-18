import { describe, expect, it, vi } from "vitest";
import { readBufferedDownload } from "./download-buffer";

function response(body, headers = {}) {
  return {
    body,
    headers: { get: (name) => headers[name] ?? null },
  };
}

describe("readBufferedDownload in a browser", () => {
  it("buffers a small stream and preserves its content type", async () => {
    const releaseLock = vi.fn();
    const reader = {
      read: vi
        .fn()
        .mockResolvedValueOnce({ done: false, value: new TextEncoder().encode("small") })
        .mockResolvedValueOnce({ done: true }),
      releaseLock,
    };

    const blob = await readBufferedDownload(response({ getReader: () => reader }, { "Content-Type": "text/plain" }));

    expect(blob.size).toBe(5);
    expect(blob.type).toBe("text/plain");
    expect(releaseLock).toHaveBeenCalledOnce();
  });

  it("rejects a declared oversized download before reading its body", async () => {
    const cancel = vi.fn().mockRejectedValue(new Error("already closed"));
    const getReader = vi.fn();

    await expect(readBufferedDownload(response({ cancel, getReader }, { "Content-Length": String(64 * 1024 * 1024 + 1) }))).rejects.toThrow(
      /64 MiB/,
    );

    expect(cancel).toHaveBeenCalledOnce();
    expect(getReader).not.toHaveBeenCalled();
  });

  it("stops an unknown-length stream before buffering past the limit", async () => {
    const chunk = new Uint8Array(1024 * 1024);
    const cancel = vi.fn().mockRejectedValue(new Error("closed"));
    const releaseLock = vi.fn();
    let reads = 0;
    const reader = {
      read: async () => {
        reads += 1;
        return { done: false, value: chunk };
      },
      cancel,
      releaseLock,
    };

    await expect(readBufferedDownload(response({ getReader: () => reader }))).rejects.toThrow(/64 MiB/);

    expect(reads).toBe(65);
    expect(cancel).toHaveBeenCalledOnce();
    expect(releaseLock).toHaveBeenCalledOnce();
  });

  it("fails closed when the browser cannot provide a readable stream", async () => {
    await expect(readBufferedDownload(response(null, { "Content-Length": "5" }))).rejects.toThrow(/streaming Save dialog/);
  });
});
