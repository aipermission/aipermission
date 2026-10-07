import assert from "node:assert/strict";
import test from "node:test";

import { isValidDatabasePassword } from "./password.ts";

test("isValidDatabasePassword requires length, uppercase, lowercase, and number", () => {
  assert.equal(isValidDatabasePassword("short"), false);
  assert.equal(isValidDatabasePassword("lowercasepassword123"), false);
  assert.equal(isValidDatabasePassword("UPPERCASEPASSWORD123"), false);
  assert.equal(isValidDatabasePassword("NoNumbersPassword"), false);
  assert.equal(isValidDatabasePassword("GoodPassword123"), true);
});

test("database password length counts Unicode code points, not bytes or UTF-16 units", () => {
  for (const [password, valid] of [
    ["Aa1" + "x".repeat(10), false],
    ["Aa1" + "x".repeat(11), true],
    ["Aa1" + "\u00e9".repeat(10), false],
    ["Aa1" + "\u00e9".repeat(11), true],
    ["Aa1" + "\u{1f680}".repeat(10), false],
    ["Aa1" + "\u{1f680}".repeat(11), true],
    ["Aa1" + "e\u0301".repeat(5), false],
    ["Aa1" + "e\u0301".repeat(5) + "x", true],
    ["aa1" + "\u00c9".repeat(11), false],
    ["Aa\u0661" + "x".repeat(11), false],
  ] as const) {
    assert.equal(isValidDatabasePassword(password), valid);
  }
});
