import assert from "node:assert/strict";
import test from "node:test";

import { createRequestGuard } from "../connectors/templates/_shared/request-guard.js";

test("request guard rejects older requests in the same channel", () => {
  const guard = createRequestGuard("target:1");
  const older = guard.begin("detail");
  const newer = guard.begin("detail");

  assert.equal(older.isCurrent(), false);
  assert.equal(newer.isCurrent(), true);
});

test("request guard rejects requests after target scope changes or disposal", () => {
  const guard = createRequestGuard("target:1");
  const previousTarget = guard.begin("list");
  guard.setScope("target:2");
  const currentTarget = guard.begin("list");

  assert.equal(previousTarget.isCurrent(), false);
  assert.equal(currentTarget.isCurrent(), true);
  guard.dispose();
  assert.equal(currentTarget.isCurrent(), false);
});

test("request guard can reactivate without reviving disposed requests", () => {
  const guard = createRequestGuard("target:1");
  const disposed = guard.begin("list");
  guard.dispose();
  guard.activate();
  const current = guard.begin("list");

  assert.equal(disposed.isCurrent(), false);
  assert.equal(current.isCurrent(), true);
});
