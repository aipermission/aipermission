import assert from "node:assert/strict";
import { pathToFileURL } from "node:url";
import test from "node:test";
import { createUploadRootResolver } from "../../src/upload-roots.js";

function fixture(options = {}) {
  let roots = options.roots ?? [{ uri: "file:///work/my-project" }];
  let changed;
  let reads = 0;
  const server = {
    getClientCapabilities: () => (options.unsupported ? {} : { roots: {} }),
    setNotificationHandler: (_schema, handler) => (changed = handler),
    listRoots: async (_params, request) => {
      reads++;
      assert.equal(request.timeout, 5000);
      request.signal.throwIfAborted();
      if (options.failure) throw new Error("Client roots unavailable");
      return { roots };
    },
  };
  return {
    resolve: createUploadRootResolver(server, { cwd: "/work/launch-project", home: "/home/operator", ...options }),
    reads: () => reads,
    change: (value) => {
      roots = value;
      changed?.();
    },
  };
}

test("client workspace roots are authoritative, local and normalized", async () => {
  const state = fixture({ roots: [{ uri: pathToFileURL("/work/my project/", { windows: false }).href }] });
  assert.deepEqual((await state.resolve()).roots, ["/work/my project"]);
  assert.equal(state.reads(), 1);
});

test("clients without roots use only the captured launch directory", async () => {
  const state = fixture({ unsupported: true });
  assert.deepEqual((await state.resolve()).roots, ["/work/launch-project"]);
  assert.equal(state.reads(), 0);
});

test("automatic roots cannot authorize home, its ancestors or filesystem root", async () => {
  for (const directory of ["/", "/home", "/home/operator"]) {
    await assert.rejects(fixture({ roots: [{ uri: pathToFileURL(directory, { windows: false }).href }] }).resolve());
    await assert.rejects(fixture({ unsupported: true, cwd: directory }).resolve());
  }
});

test("empty, invalid or unavailable advertised roots do not fall back to cwd", async () => {
  for (const roots of [
    [],
    [{ uri: "https://example.invalid/project" }],
    [{ uri: "file://remote/project" }],
    [{ uri: "file:///work/project?query" }],
    [{ uri: "file:///work/project#fragment" }],
    [{ uri: "file:///work/%2fetc" }],
  ]) {
    await assert.rejects(fixture({ roots }).resolve());
  }
  await assert.rejects(fixture({ failure: true }).resolve(), /Client roots unavailable/);
  await assert.rejects(fixture({ roots: Array.from({ length: 17 }, () => ({ uri: "file:///work/project" })) }).resolve(), /at most 16/);
});

test("configured upload roots override workspace discovery and keep explicit disable", async () => {
  const state = fixture({ configuredRoots: '["/work/release-files"]', failure: true });
  assert.deepEqual((await state.resolve()).roots, ["/work/release-files"]);
  assert.equal(state.reads(), 0);
  assert.deepEqual((await fixture({ configuredRoots: "[]" }).resolve()).roots, []);
  await assert.rejects(fixture({ configuredRoots: "broken" }).resolve(), /AIPERMISSION_UPLOAD_ROOTS/);
});

test("workspace changes invalidate in-flight scopes and use the new roots next time", async () => {
  const state = fixture();
  const first = await state.resolve();
  state.change([{ uri: "file:///work/second-project" }]);
  assert.equal(first.signal.aborted, true);
  assert.match(first.signal.reason.message, /workspace changed/);
  const second = await state.resolve();
  assert.equal(second.signal.aborted, false);
  assert.deepEqual(second.roots, ["/work/second-project"]);
  assert.equal(state.reads(), 2);
});

test("caller cancellation is preserved before root discovery", async () => {
  const state = fixture();
  const abort = new AbortController();
  abort.abort(new Error("Canceled upload"));
  await assert.rejects(state.resolve(abort.signal), /Canceled upload/);
  assert.equal(state.reads(), 0);
});
