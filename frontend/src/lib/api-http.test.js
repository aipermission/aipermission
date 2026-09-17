import assert from "node:assert/strict";
import test from "node:test";

import { apiDelete, apiDownload, apiGet, apiPost, apiPostForm, apiPut } from "./api.js";
import { APIError } from "./errors.js";

test("all API helpers forward the caller AbortSignal", async () => {
  const originalFetch = globalThis.fetch;
  const controller = new AbortController();
  const calls = [];
  globalThis.fetch = async (url, options = {}) => {
    calls.push(options);
    if (options.method === "DELETE") return response(null, 204);
    if (url.endsWith("/api/download")) return response({ error: "download unavailable" }, 503);
    return response({ ok: true });
  };
  try {
    await apiGet("/api/test", { signal: controller.signal });
    await apiPost("/api/test", {}, { signal: controller.signal });
    await apiPostForm("/api/test", new FormData(), { signal: controller.signal });
    await apiPut("/api/test", {}, { signal: controller.signal });
    await apiDelete("/api/test", { signal: controller.signal });
    await assert.rejects(() => apiDownload("/api/download", "test.txt", { signal: controller.signal }), /download unavailable/);
    assert.deepEqual([calls.length, calls.every((options) => options.signal === controller.signal)], [6, true]);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("bounded GET requests abort delayed reads without changing mutation behavior", async (t) => {
  t.mock.method(
    globalThis,
    "fetch",
    async (_url, { signal }) =>
      new Promise((_, reject) => signal.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")), { once: true })),
  );
  await assert.rejects(() => apiGet("/api/slow", { timeoutMs: 5 }), /Gateway read timed out after 5ms/);
});

test("API failures retain structured status and classification", async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async () => response({ error: "invalid input", code: "invalid_scope", details: { field: "scope" } }, 422);
  try {
    await assert.rejects(
      () => apiGet("/api/test"),
      (error) => {
        assert.equal(error instanceof APIError, true);
        assert.equal(error.name, "APIError");
        assert.equal(error.message, "invalid input");
        assert.equal(error.status, 422);
        assert.equal(error.code, "invalid_scope");
        assert.equal(error.kind, "validation");
        assert.deepEqual(error.details, { field: "scope" });
        assert.deepEqual(error.data, { error: "invalid input", code: "invalid_scope", details: { field: "scope" } });
        return true;
      },
    );
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("malformed API failures retain HTTP status and classification", async () => {
  const originalFetch = globalThis.fetch;
  try {
    for (const [body, status, kind, message] of [
      ["", 401, "authentication", /Empty JSON response/],
      ["<html>proxy unavailable</html>", 503, "unavailable", /HTML instead of JSON/],
    ]) {
      globalThis.fetch = async () => new Response(body, { status });
      await assert.rejects(
        () => apiGet("/api/test"),
        (error) => {
          assert.equal(error instanceof APIError, true);
          assert.equal(error.status, status);
          assert.equal(error.kind, kind);
          assert.match(error.message, message);
          return true;
        },
      );
    }
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("JSON API helpers reject malformed successful responses by default", async () => {
  const originalFetch = globalThis.fetch;
  try {
    for (const [body, expected] of [
      ["<html>gateway fallback</html>", /HTML instead of JSON/],
      ['{"partial":', /Invalid JSON response/],
      ["", /Empty JSON response/],
    ]) {
      globalThis.fetch = async () => new Response(body, { status: 200 });
      await assert.rejects(() => apiGet("/api/test"), expected);
      await assert.rejects(() => apiPost("/api/test", {}), expected);
      await assert.rejects(() => apiPostForm("/api/test", new FormData()), expected);
      await assert.rejects(() => apiPut("/api/test", {}), expected);
    }
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("JSON parsing precedes HTML detection and DELETE explicitly accepts 204", async () => {
  const originalFetch = globalThis.fetch;
  try {
    globalThis.fetch = async () => new Response(JSON.stringify({ value: "remote output contains <body text" }), { status: 200 });
    assert.deepEqual(await apiGet("/api/test"), { value: "remote output contains <body text" });
    globalThis.fetch = async () => new Response(null, { status: 204 });
    assert.equal(await apiDelete("/api/test"), null);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("picker downloads stream the response directly to the selected file", async () => {
  const originalFetch = globalThis.fetch;
  const originalWindow = globalThis.window;
  const writable = {};
  let destination = null;
  let suggestedName = "";
  globalThis.window = {
    showSaveFilePicker: async ({ suggestedName: value }) => {
      suggestedName = value;
      return { createWritable: async () => writable };
    },
  };
  globalThis.fetch = async () => ({
    ok: true,
    body: {
      async pipeTo(value) {
        destination = value;
      },
    },
    async blob() {
      throw new Error("streaming download must not buffer a Blob");
    },
  });
  try {
    const result = await apiDownload("/api/file-transfer-batches/1/download", "backup:latest.zip", { picker: true });
    assert.deepEqual(result, { saved: true, method: "picker" });
    assert.equal(suggestedName, "backup-latest.zip");
    assert.equal(destination, writable);
  } finally {
    globalThis.fetch = originalFetch;
    restoreWindow(originalWindow);
  }
});

test("picker downloads retain the Blob fallback when response streaming is unavailable", async () => {
  const originalFetch = globalThis.fetch;
  const originalWindow = globalThis.window;
  const blob = { size: 42 };
  const writes = [];
  let closed = false;
  globalThis.window = {
    showSaveFilePicker: async () => ({
      createWritable: async () => ({
        async write(value) {
          writes.push(value);
        },
        async close() {
          closed = true;
        },
      }),
    }),
  };
  globalThis.fetch = async () => ({ ok: true, body: null, blob: async () => blob });
  try {
    const result = await apiDownload("/api/backup/download", "backup.aipdb", { picker: true });
    assert.deepEqual(result, { saved: true, method: "picker" });
    assert.deepEqual(writes, [blob]);
    assert.equal(closed, true);
  } finally {
    globalThis.fetch = originalFetch;
    restoreWindow(originalWindow);
  }
});

function response(body, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    async text() {
      return JSON.stringify(body);
    },
  };
}

function restoreWindow(value) {
  if (value === undefined) delete globalThis.window;
  else globalThis.window = value;
}
