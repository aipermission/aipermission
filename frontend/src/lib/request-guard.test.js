import assert from "node:assert/strict";
import test from "node:test";

import { createRequestGuard } from "./request-guard.js";

test("request guard rejects older requests in the same channel", () => {
  const guard = createRequestGuard("target:1");
  const older = guard.begin("detail");
  const newer = guard.begin("detail");

  assert.equal(older.signal.aborted, true);
  assert.equal(newer.signal.aborted, false);
  assert.equal(older.isCurrent(), false);
  assert.equal(newer.isCurrent(), true);
});

test("request guard rejects requests after target scope changes or disposal", () => {
  const guard = createRequestGuard("target:1");
  const previousTarget = guard.begin("list");
  guard.setScope("target:2");
  const currentTarget = guard.begin("list");

  assert.equal(previousTarget.signal.aborted, true);
  assert.equal(previousTarget.isCurrent(), false);
  assert.equal(currentTarget.isCurrent(), true);
  guard.dispose();
  assert.equal(currentTarget.signal.aborted, true);
  assert.equal(currentTarget.isCurrent(), false);
});

test("request guard rejects an old response after scope returns to the same value", () => {
  const guard = createRequestGuard("target:a");
  const firstA = guard.begin("detail");
  guard.setScope("target:b");
  guard.setScope("target:a");
  const currentA = guard.begin("detail");

  assert.equal(firstA.isCurrent(), false);
  assert.equal(currentA.isCurrent(), true);
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

test("request guard aborts only the invalidated channel", () => {
  const guard = createRequestGuard("target:1");
  const list = guard.begin("list");
  const detail = guard.begin("detail");

  guard.invalidate("list");

  assert.equal(list.signal.aborted, true);
  assert.equal(list.isCurrent(), false);
  assert.equal(detail.signal.aborted, false);
  assert.equal(detail.isCurrent(), true);
});

test("request guard scope changes abort every in-flight channel", () => {
  const guard = createRequestGuard("target:1");
  const list = guard.begin("list");
  const detail = guard.begin("detail");

  guard.setScope("target:2");

  assert.equal(list.signal.aborted, true);
  assert.equal(detail.signal.aborted, true);
  assert.equal(list.isCurrent(), false);
  assert.equal(detail.isCurrent(), false);
});

test("completed requests release cancellation ownership without reviving stale work", () => {
  const guard = createRequestGuard("target:1");
  const completed = guard.begin("list");

  completed.complete();
  guard.setScope("target:2");

  assert.equal(completed.signal.aborted, false);
  assert.equal(completed.isCurrent(), false);
});
