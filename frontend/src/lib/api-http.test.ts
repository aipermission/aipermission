import assert from "node:assert/strict";
import test from "node:test";

import { apiDelete, apiDownload, apiGet, apiPost, apiPostForm, apiPut } from "./api.ts";
import { APIError } from "./errors.ts";

test("all API helpers forward the caller AbortSignal", async () => {
  const originalFetch = globalThis.fetch;
  const controller = new AbortController();
  const calls: RequestInit[] = [];
  Reflect.set(globalThis, "fetch", async (url: RequestInfo | URL, options: RequestInit = {}) => {
    calls.push(options);
    if (options.method === "DELETE") return response(null, 204);
    if (String(url).endsWith("/api/download")) return response({ error: "download unavailable" }, 503);
    return response({ ok: true });
  });
  try {
    await apiGet("/api/test", { signal: controller.signal });
    await apiPost("/api/test", {}, { signal: controller.signal });
    await apiPostForm("/api/test", new FormData(), { signal: controller.signal });
    await apiPut("/api/test", {}, { signal: controller.signal });
    await apiDelete("/api/test", { signal: controller.signal });
    await assert.rejects(() => apiDownload("/api/download", "test.txt", { signal: controller.signal }), /download unavailable/);
    assert.deepEqual([calls.length, calls.every((options) => options.signal === controller.signal)], [6, true]);
  } finally {
    Reflect.set(globalThis, "fetch", originalFetch);
  }
});

test("a rejected stale download cannot adopt another workspace for retry", async () => {
  const originalFetch = globalThis.fetch;
  const originalWindow = globalThis.window;
  const originalDocument = globalThis.document;
  const requestBindings: (string | null)[] = [];
  Reflect.set(globalThis, "window", { location: { protocol: "http:", port: "3210" } });
  Reflect.set(globalThis, "document", { cookie: "aipermission_workspace_3210=workspace-a" });
  Reflect.set(globalThis, "fetch", async (_url: RequestInfo | URL, options: RequestInit = {}) => {
    requestBindings.push(new Headers(options.headers).get("X-AIPermission-Workspace"));
    return new Response(JSON.stringify({ error: "workspace changed" }), {
      status: 409,
      headers: {
        "Content-Type": "application/json",
        "X-AIPermission-Workspace": "workspace-b",
        "X-AIPermission-Workspace-Changed": "true",
      },
    });
  });
  try {
    await assert.rejects(() => apiDownload("/api/backup/download", "backup.aipdb"), /workspace changed/);
    await assert.rejects(() => apiDownload("/api/backup/download", "backup.aipdb"), /workspace changed/);
    assert.deepEqual(requestBindings, ["workspace-a", "workspace-a"]);
  } finally {
    Reflect.set(globalThis, "fetch", originalFetch);
    restoreWindow(originalWindow);
    if (originalDocument === undefined) Reflect.deleteProperty(globalThis, "document");
    else Reflect.set(globalThis, "document", originalDocument);
  }
});

test("bounded GET requests abort delayed reads without changing mutation behavior", async (t) => {
  t.mock.method(globalThis, "fetch", async (_url: RequestInfo | URL, { signal }: RequestInit) => {
    assert.ok(signal);
    return new Promise<Response>((_, reject) =>
      signal.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")), { once: true }),
    );
  });
  await assert.rejects(() => apiGet("/api/slow", { timeoutMs: 5 }), /Gateway read timed out after 5ms/);
});

test("API failures retain structured status and classification", async () => {
  const originalFetch = globalThis.fetch;
  Reflect.set(globalThis, "fetch", async () =>
    response({ error: "invalid input", code: "invalid_scope", details: { field: "scope" } }, 422),
  );
  try {
    await assert.rejects(
      () => apiGet("/api/test"),
      (error) => {
        assert.ok(error instanceof APIError);
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
    Reflect.set(globalThis, "fetch", originalFetch);
  }
});

test("malformed API failures retain HTTP status and classification", async () => {
  const originalFetch = globalThis.fetch;
  try {
    for (const [body, status, kind, message] of [
      ["", 401, "authentication", /Empty JSON response/],
      ["<html>proxy unavailable</html>", 503, "unavailable", /HTML instead of JSON/],
    ] as const) {
      Reflect.set(globalThis, "fetch", async () => new Response(body, { status }));
      await assert.rejects(
        () => apiGet("/api/test"),
        (error) => {
          assert.ok(error instanceof APIError);
          assert.equal(error.status, status);
          assert.equal(error.kind, kind);
          assert.match(error.message, message);
          return true;
        },
      );
    }
  } finally {
    Reflect.set(globalThis, "fetch", originalFetch);
  }
});

test("JSON API helpers reject malformed successful responses by default", async () => {
  const originalFetch = globalThis.fetch;
  try {
    for (const [body, expected] of [
      ["<html>gateway fallback</html>", /HTML instead of JSON/],
      ['{"partial":', /Invalid JSON response/],
      ["", /Empty JSON response/],
    ] as const) {
      Reflect.set(globalThis, "fetch", async () => new Response(body, { status: 200 }));
      await assert.rejects(() => apiGet("/api/test"), expected);
      await assert.rejects(() => apiPost("/api/test", {}), expected);
      await assert.rejects(() => apiPostForm("/api/test", new FormData()), expected);
      await assert.rejects(() => apiPut("/api/test", {}), expected);
    }
  } finally {
    Reflect.set(globalThis, "fetch", originalFetch);
  }
});

test("JSON parsing precedes HTML detection and DELETE explicitly accepts 204", async () => {
  const originalFetch = globalThis.fetch;
  try {
    Reflect.set(
      globalThis,
      "fetch",
      async () => new Response(JSON.stringify({ value: "remote output contains <body text" }), { status: 200 }),
    );
    assert.deepEqual(await apiGet("/api/test"), { value: "remote output contains <body text" });
    Reflect.set(globalThis, "fetch", async () => new Response(null, { status: 204 }));
    assert.equal(await apiDelete("/api/test"), null);
  } finally {
    Reflect.set(globalThis, "fetch", originalFetch);
  }
});

test("picker downloads stream the response directly to the selected file", async () => {
  const originalFetch = globalThis.fetch;
  const originalWindow = globalThis.window;
  const writable = {};
  let destination: unknown = null;
  let suggestedName = "";
  Reflect.set(globalThis, "window", {
    showSaveFilePicker: async ({ suggestedName: value }: { suggestedName: string }) => {
      suggestedName = value;
      return { createWritable: async () => writable };
    },
  });
  Reflect.set(globalThis, "fetch", async () => ({
    ok: true,
    body: {
      async pipeTo(value: unknown) {
        destination = value;
      },
    },
    async blob() {
      throw new Error("streaming download must not buffer a Blob");
    },
  }));
  try {
    const result = await apiDownload("/api/file-transfer-batches/1/download", "backup:latest.zip", { picker: true });
    assert.deepEqual(result, { saved: true, method: "picker" });
    assert.equal(suggestedName, "backup-latest.zip");
    assert.equal(destination, writable);
  } finally {
    Reflect.set(globalThis, "fetch", originalFetch);
    restoreWindow(originalWindow);
  }
});

test("picker downloads reject unavailable response streams without buffering", async () => {
  const originalFetch = globalThis.fetch;
  const originalWindow = globalThis.window;
  Reflect.set(globalThis, "window", {
    showSaveFilePicker: async () => ({
      createWritable: async () => {
        throw new Error("must not open a writer");
      },
    }),
  });
  Reflect.set(globalThis, "fetch", async () => ({
    ok: true,
    headers: new Headers({ "Content-Length": "42" }),
    body: null,
    blob: async () => {
      throw new Error("must not buffer");
    },
  }));
  try {
    await assert.rejects(() => apiDownload("/api/backup/download", "backup.aipdb", { picker: true }), /streaming Save dialog/);
  } finally {
    Reflect.set(globalThis, "fetch", originalFetch);
    restoreWindow(originalWindow);
  }
});

test("picker downloads cancel the response when opening the destination fails", async () => {
  const originalFetch = globalThis.fetch;
  const originalWindow = globalThis.window;
  let cancelCalls = 0;
  const cancel = async () => {
    cancelCalls += 1;
  };
  Reflect.set(globalThis, "window", {
    showSaveFilePicker: async () => ({
      createWritable: async () => {
        throw new Error("destination unavailable");
      },
    }),
  });
  Reflect.set(globalThis, "fetch", async () => ({ ok: true, body: { pipeTo: async () => {}, cancel } }));
  try {
    await assert.rejects(() => apiDownload("/api/backup/download", "backup.aipdb", { requireStreaming: true }), /destination unavailable/);
    assert.equal(cancelCalls, 1);
  } finally {
    Reflect.set(globalThis, "fetch", originalFetch);
    restoreWindow(originalWindow);
  }
});

test("picker downloads abort the destination after a streaming write failure", async () => {
  const originalFetch = globalThis.fetch;
  const originalWindow = globalThis.window;
  let abortCalls = 0;
  let cancelCalls = 0;
  const abort = async () => {
    abortCalls += 1;
  };
  const cancel = async () => {
    cancelCalls += 1;
  };
  Reflect.set(globalThis, "window", {
    showSaveFilePicker: async () => ({ createWritable: async () => ({ abort }) }),
  });
  Reflect.set(globalThis, "fetch", async () => ({
    ok: true,
    body: {
      async pipeTo() {
        throw new Error("stream write failed");
      },
      cancel,
    },
  }));
  try {
    await assert.rejects(() => apiDownload("/api/backup/download", "backup.aipdb", { requireStreaming: true }), /stream write failed/);
    assert.equal(abortCalls, 1);
    assert.equal(cancelCalls, 1);
  } finally {
    Reflect.set(globalThis, "fetch", originalFetch);
    restoreWindow(originalWindow);
  }
});

test("required streaming downloads fail before fetching when the native picker is unavailable", async () => {
  const originalFetch = globalThis.fetch;
  const originalWindow = globalThis.window;
  const fetchCalls: unknown[][] = [];
  Reflect.set(globalThis, "window", {});
  Reflect.set(globalThis, "fetch", async (...args: unknown[]) => {
    fetchCalls.push(args);
    throw new Error("fetch must not start");
  });
  try {
    await assert.rejects(
      () => apiDownload("/api/backup/download", "backup.aipdb", { picker: true, requireStreaming: true }),
      /requires a browser with a streaming Save dialog/,
    );
    assert.equal(fetchCalls.length, 0);
  } finally {
    Reflect.set(globalThis, "fetch", originalFetch);
    restoreWindow(originalWindow);
  }
});

test("required streaming implies picker use and preserves compatibility errors when cancellation fails", async () => {
  const originalFetch = globalThis.fetch;
  const originalWindow = globalThis.window;
  let pickerCalls = 0;
  Reflect.set(globalThis, "window", {
    showSaveFilePicker: async () => {
      pickerCalls += 1;
      return { createWritable: async () => ({}) };
    },
  });
  Reflect.set(globalThis, "fetch", async () => ({
    ok: true,
    body: {
      async cancel() {
        throw new Error("cancel failed");
      },
    },
  }));
  try {
    await assert.rejects(
      () => apiDownload("/api/backup/download", "backup.aipdb", { requireStreaming: true }),
      /cannot stream this download/,
    );
    assert.equal(pickerCalls, 1);
  } finally {
    Reflect.set(globalThis, "fetch", originalFetch);
    restoreWindow(originalWindow);
  }
});

function response(body: unknown, status = 200) {
  return new Response(status === 204 ? null : JSON.stringify(body), { status });
}

function restoreWindow(value: unknown) {
  if (value === undefined) Reflect.deleteProperty(globalThis, "window");
  else Reflect.set(globalThis, "window", value);
}
