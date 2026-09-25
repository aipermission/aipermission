import assert from "node:assert/strict";
import test from "node:test";
import { joinTransferPath, normalizeTransferDirectory } from "./transfer-paths.ts";
import { rememberDownloadPath, rememberedDownloadPath } from "../../../lib/file-transfer-utils.js";

test("transfer path callbacks preserve opaque prefix and filename components", () => {
  for (const value of ["/a//", "//a/", "/a/../", "/ space "]) {
    assert.equal(normalizeTransferDirectory(value), value);
    assert.equal(joinTransferPath(value, " name "), `${value}${value.endsWith("/") ? "" : "/"} name `);
  }
  assert.equal(joinTransferPath("/", "/a"), "//a");
});

test("remembered directory uses the supplied identity policy", () => {
  const original = Object.getOwnPropertyDescriptor(globalThis, "window");
  const values = new Map<string, string>();
  Object.defineProperty(globalThis, "window", {
    configurable: true,
    value: { localStorage: { setItem: (key: string, value: string) => values.set(key, value), getItem: (key: string) => values.get(key) } },
  });
  try {
    rememberDownloadPath({ id: 7 }, "//a// ", normalizeTransferDirectory);
    assert.equal(rememberedDownloadPath({ id: 7 }, "/", normalizeTransferDirectory), "//a// ");
  } finally {
    if (original === undefined) Reflect.deleteProperty(globalThis, "window");
    else Object.defineProperty(globalThis, "window", original);
  }
});
