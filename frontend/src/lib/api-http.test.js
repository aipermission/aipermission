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
    assert.equal(calls.length, 6);
    assert.equal(
      calls.every((options) => options.signal === controller.signal),
      true,
    );
  } finally {
    globalThis.fetch = originalFetch;
  }
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

test("multipart callers can require a JSON response", async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async () => new Response("<html>gateway fallback</html>", { status: 200 });
  try {
    await assert.rejects(() => apiPostForm("/api/test", new FormData(), { requireJSON: true }), /HTML instead of JSON/);
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
