import assert from "node:assert/strict";
import test from "node:test";
import { joinTransferPath, normalizeTransferDirectory } from "./transfer-paths.js";
import { rememberDownloadPath, rememberedDownloadPath } from "../../../lib/file-transfer-utils.js";

test("transfer path callbacks preserve opaque prefix and filename components", () => {
  for (const value of ["/a//", "//a/", "/a/../", "/ space "]) {
    assert.equal(normalizeTransferDirectory(value), value);
    assert.equal(joinTransferPath(value, " name "), `${value}${value.endsWith("/") ? "" : "/"} name `);
  }
  assert.equal(joinTransferPath("/", "/a"), "//a");
});

test("remembered directory uses the supplied identity policy", () => {
  const original = globalThis.window;
  const values = new Map();
  globalThis.window = { localStorage: { setItem: (key, value) => values.set(key, value), getItem: (key) => values.get(key) } };
  try {
    rememberDownloadPath({ id: 7 }, "//a// ", normalizeTransferDirectory);
    assert.equal(rememberedDownloadPath({ id: 7 }, "/", normalizeTransferDirectory), "//a// ");
  } finally {
    if (original === undefined) delete globalThis.window;
    else globalThis.window = original;
  }
});
