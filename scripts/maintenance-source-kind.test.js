const assert = require("node:assert/strict");
const test = require("node:test");

const { isTestSource } = require("./maintenance-source-kind");

const frontendMarkers = [".test.", ".spec."];

test("classifies test modules according to their ecosystem", () => {
  assert.equal(
    isTestSource("backend", "/tmp/service_test.go", frontendMarkers),
    true,
  );
  assert.equal(
    isTestSource("backend", "/tmp/runtime.test.fixture.go", frontendMarkers),
    false,
  );
  assert.equal(
    isTestSource(
      "frontend/src",
      "/tmp/runtime.test.fixture.js",
      frontendMarkers,
    ),
    true,
  );
  assert.equal(
    isTestSource("packages/mcp/src", "/tmp/client.spec.ts", frontendMarkers),
    true,
  );
  assert.equal(
    isTestSource("packages/mcp/src", "/tmp/client.fixture.ts", frontendMarkers),
    false,
  );
  assert.equal(
    isTestSource("packages/mcp/test", "/tmp/client.test.js", frontendMarkers),
    true,
  );
});

test("rejects unknown budget ecosystems instead of silently weakening policy", () => {
  assert.throws(
    () => isTestSource("new/runtime", "/tmp/service.test.js", frontendMarkers),
    /unknown maintenance source directory/,
  );
});
