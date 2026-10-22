import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { createRequire } from "node:module";
import test from "node:test";

test("MCP SDK address dependency rejects oversized malformed IPv6 zones without blocking", () => {
  const fromSDK = createRequire(import.meta.resolve("@modelcontextprotocol/sdk/server/index.js"));
  const fromLimiter = createRequire(fromSDK.resolve("express-rate-limit"));
  const script = `
    const assert = require("node:assert/strict");
    const { Address4, Address6 } = require(process.argv[1]);
    assert.equal(Address4.isValid("203.0.113.10"), true);
    assert.equal(Address6.isValid("2001:db8::1"), true);
    assert.equal(new Address6("fe80::1%eth0").zone, "%eth0");
    for (const ending of ["\\n", "\\r", "\\u2028", "\\u2029"]) {
      const input = "%".repeat(1024 * 1024) + ending;
      assert.equal(Address6.isValid(input), false);
      assert.throws(() => new Address6(input), { name: "AddressError" });
    }
  `;
  const result = spawnSync(process.execPath, ["-e", script, fromLimiter.resolve("ip-address")], {
    encoding: "utf8",
    timeout: 10_000,
    maxBuffer: 64 * 1024,
    windowsHide: true,
  });
  assert.ifError(result.error);
  assert.equal(result.status, 0, result.stderr);
});
