import assert from "node:assert/strict";
import { createRequire } from "node:module";
import test from "node:test";

const fromVite = createRequire(import.meta.resolve("vite"));
const fromPostcss = createRequire(fromVite.resolve("postcss"));
const { SourceMapConsumer } = fromPostcss("source-map-js");
const basic = { version: 3, sources: ["fixture.js"], names: [], mappings: "AAAA" };
const indexed = (line: unknown, column: unknown = 0, map: unknown = basic) => ({
  version: 3,
  sections: [{ offset: { line, column }, map }],
});

test("build source maps reject invalid and excessive indexed section offsets", () => {
  for (const invalid of [-1, 0.5, "1", NaN, Infinity, Number.MAX_SAFE_INTEGER + 1]) {
    assert.throws(() => new SourceMapConsumer(indexed(invalid)), /Section offset/);
    assert.throws(() => new SourceMapConsumer(indexed(0, invalid)), /Section offset/);
  }
  // Constructor checks fail fast without executing a potentially blocking mapping loop.
  assert.throws(() => new SourceMapConsumer(indexed(1e10)), /Section offset/);
  assert.throws(() => new SourceMapConsumer(indexed(6e6, 0, indexed(6e6))), /nested sections/);
  const consumer = new SourceMapConsumer(indexed(2));
  assert.deepEqual(consumer.generatedPositionFor({ source: "fixture.js", line: 1, column: 0 }), { line: 3, column: 0 });
});
