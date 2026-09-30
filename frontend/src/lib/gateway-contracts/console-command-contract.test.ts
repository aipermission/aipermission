import assert from "node:assert/strict";
import { test } from "node:test";
import {
  consoleCommandBatch,
  consoleCommandDetail,
  isDefinitiveConsoleCommandStatus,
  isUnknownConsoleCommandStatus,
  validCommandBatchIdentities,
} from "./console-command-contract.ts";

const item = { request_id: 1, target_id: 2, target_name: "Example host", status: "running" };
test("batch validation rejects malformed, duplicate, out-of-budget or wrong-target command identities", () => {
  const good = { parallelism: 3, items: [item] };
  assert.deepEqual(consoleCommandBatch(good, [2]), good);
  for (const value of [
    null,
    [],
    {},
    { ...good, request_id: 1, target_ref: "example:1:1", action_name: "mutate" },
    { ...good, parallelism: 0 },
    { ...good, parallelism: 26 },
    { ...good, items: [] },
    { ...good, items: [item, item] },
    { ...good, items: [{ ...item, status: "future" }] },
    { ...good, items: [{ ...item, stdout: {} }] },
    { ...good, items: [{ ...item, request_id: "1" }] },
    { ...good, items: [{ ...item, target_id: 0 }] },
    { ...good, items: [{ ...item, observed: "true" }] },
    { ...good, items: Array.from({ length: 26 }, (_, index) => ({ ...item, request_id: index + 1, target_id: index + 1 })) },
  ])
    assert.throws(() => consoleCommandBatch(value));
  assert.throws(() => consoleCommandBatch(good, [99]), /target identity/);
  assert.throws(() => consoleCommandBatch(good, [2, 3]), /target identity/);
  assert.equal(validCommandBatchIdentities([item, { ...item, request_id: 2 }]), false);
});

test("detail validation requires the exact request ID and a positive numeric runtime identity", () => {
  const good = { id: 1, runtime_id: 2, status: "completed", exit_code: 0, stdout: "done" };
  assert.deepEqual(consoleCommandDetail(good, 1), good);
  for (const value of [
    null,
    {},
    { ...good, id: 99 },
    { ...good, runtime_id: "2" },
    { ...good, runtime_id: 0 },
    { ...good, status: "future" },
    { ...good, stderr: [] },
    { ...good, exit_code: 1.5 },
  ])
    assert.throws(() => consoleCommandDetail(value, 1));
});

test("only known definitive outcomes release ownership; untracked is explicitly uncertain", () => {
  for (const status of ["completed", "failed", "error", "declined", "blocked", "canceled", "stale"])
    assert.equal(isDefinitiveConsoleCommandStatus(status), true);
  for (const status of ["running", "approval_pending", "outcome_unknown", "untracked", "future", null])
    assert.equal(isDefinitiveConsoleCommandStatus(status), false);
  assert.equal(isUnknownConsoleCommandStatus("outcome_unknown"), true);
  assert.equal(isUnknownConsoleCommandStatus("untracked"), true);
  assert.equal(isUnknownConsoleCommandStatus("failed"), false);
});
