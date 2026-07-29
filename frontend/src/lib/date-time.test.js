import assert from "node:assert/strict";
import test from "node:test";
import { formatRelativeDeadline, toLocalDateTime, toRFC3339 } from "./date-time.js";

test("date helpers normalize editor and API timestamps", () => {
  assert.equal(toRFC3339("2026-07-30T09:15"), new Date("2026-07-30T09:15").toISOString());
  assert.match(toLocalDateTime("2026-07-30T06:15:00Z"), /^2026-07-30T/);
  assert.equal(toRFC3339("not-a-date"), "");
  assert.equal(toLocalDateTime("not-a-date"), "");
});

test("relative deadline reports expired values", () => {
  assert.equal(formatRelativeDeadline("2000-01-01T00:00:00Z"), "expired");
});
