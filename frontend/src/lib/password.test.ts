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
