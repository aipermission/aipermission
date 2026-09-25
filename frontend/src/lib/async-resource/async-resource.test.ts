import assert from "node:assert/strict";
import test from "node:test";
import { failedResource, pollReadOptions } from "../async-resource.ts";

test("failed resources retain their data and accept scoped overrides", () => {
  const current = { state: "ready", data: [1, 2], error: null as string | null, errors: ["old"] };
  assert.deepEqual(failedResource(current, new Error("unavailable"), { errors: [] }), {
    state: "error",
    data: [1, 2],
    error: "unavailable",
    errors: [],
  });
});

test("poll reads use a bounded timeout only when a generation is supplied", () => {
  const signal = new AbortController().signal;
  assert.deepEqual(pollReadOptions(signal), { signal });
  assert.deepEqual(pollReadOptions(signal, 0), { signal, timeoutMs: 4000 });
});
