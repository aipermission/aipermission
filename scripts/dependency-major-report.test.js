#!/usr/bin/env node

const assert = require("node:assert/strict");
const { major, renderReport } = require("./dependency-major-report");

assert.equal(major("v2.4.1"), 2);
assert.equal(major("workspace:*"), null);

const report = renderReport([
  { ecosystem: "npm", directory: "frontend", name: "example", current: "1.4.0", latest: "2.0.0" },
]);
assert.match(report, /\| npm \| `frontend` \| `example` \| `1\.4\.0` \| `2\.0\.0` \|/);
assert.match(report, /never create or merge bot-authored commits/);

console.log("Dependency major report tests passed.");
