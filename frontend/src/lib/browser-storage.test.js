import assert from "node:assert/strict";
import test from "node:test";
import { readLocalPreference, removeLocalPreference, writeLocalPreference } from "./browser-storage.js";

test("local preferences tolerate unavailable storage and preserve normal behavior", () => {
  const previousWindow = globalThis.window;
  try {
    globalThis.window = {
      get localStorage() {
        throw new DOMException("Storage denied", "SecurityError");
      },
    };
    assert.deepEqual(
      [readLocalPreference("theme"), writeLocalPreference("theme", "dark"), removeLocalPreference("theme")],
      [null, false, false],
    );

    const values = new Map();
    globalThis.window = {
      localStorage: {
        getItem: (key) => values.get(key) ?? null,
        setItem: (key, value) => values.set(key, value),
        removeItem: (key) => values.delete(key),
      },
    };
    assert.equal(writeLocalPreference("theme", "light"), true);
    assert.equal(readLocalPreference("theme"), "light");
    assert.equal(removeLocalPreference("theme"), true);
    assert.equal(readLocalPreference("theme"), null);
  } finally {
    globalThis.window = previousWindow;
  }
});
