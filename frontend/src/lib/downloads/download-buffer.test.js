import assert from "node:assert/strict";
import test from "node:test";

import { readBufferedDownload } from "./download-buffer.js";

test("buffered downloads accept a small response stream", async () => {
  const response = new Response("small download", { headers: { "Content-Type": "text/plain" } });
  const blob = await readBufferedDownload(response);
  assert.equal(await blob.text(), "small download");
  assert.equal(blob.type, "text/plain");
});

test("buffered downloads reject an oversized declared length before buffering", async () => {
  let canceled = false;
  const response = {
    headers: new Headers({ "Content-Length": String(64 * 1024 * 1024 + 1) }),
    body: {
      async cancel() {
        canceled = true;
      },
    },
    async blob() {
      throw new Error("must not buffer");
    },
  };
  await assert.rejects(() => readBufferedDownload(response), /64 MiB/);
  assert.equal(canceled, true);
});

test("buffered downloads stop an unknown-length stream at the limit", async () => {
  const chunk = new Uint8Array(1024 * 1024);
  let count = 0;
  let canceled = false;
  const stream = new ReadableStream({
    pull(controller) {
      controller.enqueue(chunk);
      count += 1;
    },
    cancel() {
      canceled = true;
    },
  });
  await assert.rejects(() => readBufferedDownload(new Response(stream)), /64 MiB/);
  assert.equal(count <= 66, true);
  assert.equal(canceled, true);
});

test("buffered downloads reject an unavailable stream even with a small declared length", async () => {
  const response = { headers: new Headers({ "Content-Length": "5" }), body: null, blob: async () => new Blob(["small"]) };
  await assert.rejects(() => readBufferedDownload(response), /streaming Save dialog/);
});
