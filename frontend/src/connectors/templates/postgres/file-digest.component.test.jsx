import { createHash, webcrypto } from "node:crypto";
import { afterEach, expect, it, vi } from "vitest";
import { fileSHA256 } from "../../../lib/file-digest";

afterEach(() => vi.unstubAllGlobals());

it("hashes file content with SHA-256", async () => {
  vi.stubGlobal("crypto", webcrypto);
  const content = new TextEncoder().encode("SELECT 1;");
  const file = { arrayBuffer: async () => content.buffer };

  await expect(fileSHA256(file)).resolves.toBe(createHash("sha256").update(content).digest("hex"));
});

it("does not read a file after cancellation", async () => {
  const arrayBuffer = vi.fn();
  await expect(fileSHA256({ arrayBuffer }, AbortSignal.abort())).rejects.toMatchObject({ name: "AbortError" });
  expect(arrayBuffer).not.toHaveBeenCalled();
});

it("fails closed when secure hashing is unavailable", async () => {
  vi.stubGlobal("crypto", {});

  await expect(fileSHA256(new Blob(["SELECT 1;"]))).rejects.toThrow("Secure file hashing is unavailable; the file was not uploaded.");
});

it("stops after file reading when cancellation arrives during the read", async () => {
  vi.stubGlobal("crypto", webcrypto);
  const controller = new AbortController();
  const file = {
    arrayBuffer: async () => {
      controller.abort();
      return new ArrayBuffer(0);
    },
  };

  await expect(fileSHA256(file, controller.signal)).rejects.toMatchObject({ name: "AbortError" });
});

it("stops after hashing when cancellation arrives during the digest", async () => {
  const controller = new AbortController();
  const digest = vi.fn(async () => {
    controller.abort();
    return new Uint8Array([0]).buffer;
  });
  vi.stubGlobal("crypto", { subtle: { digest } });

  await expect(fileSHA256(new Blob(["SELECT 1;"]), controller.signal)).rejects.toMatchObject({
    name: "AbortError",
  });
  expect(digest).toHaveBeenCalledOnce();
});
