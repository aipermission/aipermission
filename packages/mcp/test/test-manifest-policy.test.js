import assert from "node:assert/strict";
import test from "node:test";

import {
  readTestManifest,
  requiredMinimumTests,
  requiredWindowsACLTest,
  verifyRequiredWindowsACLSource,
  verifyTestManifestRatchet,
} from "../scripts/test-manifest-policy.js";

function manifest(overrides = {}) {
  return {
    files: ["test/private-file.test.js"],
    minimumTests: requiredMinimumTests,
    maximumSkipped: 1,
    windowsACLTests: [requiredWindowsACLTest, "helper rejects inherited ACLs"],
    ...overrides,
  };
}

test("MCP test manifest rejects missing immutable Windows ACL coverage", () => {
  const candidate = manifest({ windowsACLTests: ["helper rejects inherited ACLs"] });
  assert.throws(() => readTestManifest(JSON.stringify(candidate)), /missing required Windows ACL integration test/);
  assert.throws(() => verifyRequiredWindowsACLSource('test("other", () => {})'), /Missing fail-closed Windows ACL/);
});

test("MCP test manifest ratchet rejects simultaneous test and threshold weakening", () => {
  const previous = manifest();
  const candidate = manifest({
    files: [],
    minimumTests: requiredMinimumTests - 1,
    maximumSkipped: 2,
    windowsACLTests: [requiredWindowsACLTest],
  });
  assert.throws(
    () => verifyTestManifestRatchet(previous, candidate),
    /files removed required entry.*windowsACLTests removed required entry.*minimumTests decreased.*maximumSkipped increased/s,
  );
});

test("MCP test manifest ratchet permits additive strengthening", () => {
  const previous = manifest();
  const candidate = manifest({
    files: [...previous.files, "test/new.test.js"],
    minimumTests: requiredMinimumTests + 2,
    maximumSkipped: 0,
    windowsACLTests: [...previous.windowsACLTests, "new ACL regression"],
  });
  assert.doesNotThrow(() => verifyTestManifestRatchet(previous, candidate));
});

test("MCP test manifest parser rejects malformed shapes", () => {
  for (const source of [
    "not-json",
    JSON.stringify(manifest({ files: [] })),
    JSON.stringify(manifest({ files: ["duplicate", "duplicate"] })),
    JSON.stringify(manifest({ minimumTests: requiredMinimumTests - 1 })),
    JSON.stringify(manifest({ maximumSkipped: -1 })),
    JSON.stringify(manifest({ maximumSkipped: 2 })),
  ]) {
    assert.throws(() => readTestManifest(source));
  }
});
