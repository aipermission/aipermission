import assert from "node:assert/strict";
import test from "node:test";

import { retryCoverageFiles, runRetryCoverage } from "./retry-coverage.mjs";

test("discovers every retry production module", () => {
  assert.deepEqual(
    retryCoverageFiles().map((file) => file.split("/").at(-1)),
    ["local-action-retry.ts", "constants.ts", "entries.ts", "errors.ts", "records.ts", "runtime.ts", "signing.ts", "storage.ts"],
  );
});

test("checks retry coverage one production file at a time", () => {
  const checked = [];
  const status = runRetryCoverage({
    log() {},
    spawn: (_command, args) => {
      const include = args.find((argument) => argument.startsWith("--test-coverage-include="));
      checked.push(include);
      assert.deepEqual(args.slice(-3), ["src/lib/api.test.js", "src/lib/api-backup-retry.test.js", "src/lib/local-action-retry/records.test.ts"]);
      return { status: include.endsWith("errors.ts") ? 1 : 0, stdout: "", stderr: "" };
    },
  });
  assert.equal(status, 1);
  assert.equal(
    checked.every((include) => !include.includes("*")),
    true,
  );
  assert.equal(checked.at(-1), "--test-coverage-include=src/lib/local-action-retry/errors.ts");
});
