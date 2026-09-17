import assert from "node:assert/strict";
import test from "node:test";
import { readLocalPreference, removeLocalPreference, writeLocalPreference } from "./browser-storage.js";

test("local preferences fall back when browser storage is unavailable", () => {
  const previousWindow = globalThis.window;
  globalThis.window = {
    get localStorage() {
      throw new DOMException("Storage denied", "SecurityError");
    },
  };
  try {
    assert.equal(readLocalPreference("theme"), null);
    assert.equal(writeLocalPreference("theme", "dark"), false);
    assert.equal(removeLocalPreference("theme"), false);
  } finally {
    globalThis.window = previousWindow;
  }
});

test("local preferences preserve normal storage behavior", () => {
  const previousWindow = globalThis.window;
  const values = new Map();
  globalThis.window = {
    localStorage: {
      getItem: (key) => values.get(key) ?? null,
      setItem: (key, value) => values.set(key, value),
      removeItem: (key) => values.delete(key),
    },
  };
  try {
    assert.equal(writeLocalPreference("theme", "light"), true);
    assert.equal(readLocalPreference("theme"), "light");
    assert.equal(removeLocalPreference("theme"), true);
    assert.equal(readLocalPreference("theme"), null);
  } finally {
    globalThis.window = previousWindow;
  }
});
