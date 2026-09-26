import assert from "node:assert/strict";
import test from "node:test";
import { databaseStatusResponse } from "./database-status-contract.ts";

test("database status keeps database identities and lock state independent", () => {
  const databases = [{ id: "one", name: "Work", unlocked: true, current: true }, { id: "two", name: "Personal", unlocked: false, current: false }];
  assert.deepEqual(databaseStatusResponse({ state: "session_required", database_id: "one", database_name: "Work", databases }), {
    state: "session_required", database_id: "one", database_name: "Work", unlocked: undefined, databases,
  });
  assert.deepEqual(databaseStatusResponse({ databases: null }).databases, []);
});

test("database status rejects malformed UI fields and catalog items", () => {
  for (const value of [null, [], { state: {} }, { database_id: 1 }, { database_name: null }, { unlocked: "yes" }, { databases: {} }, { databases: [null] }, { databases: [{ id: "one", name: "One", unlocked: "true" }] }, { databases: [{ id: "", name: "One", unlocked: true }] }, { databases: [{ id: "one", name: "One", unlocked: true, current: "true" }] }]) {
    assert.throws(() => databaseStatusResponse(value), /Invalid database/);
  }
});
