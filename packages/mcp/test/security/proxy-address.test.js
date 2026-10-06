import assert from "node:assert/strict";
import { createRequire } from "node:module";
import test from "node:test";

test("MCP SDK proxy dependency rejects IPv4 spoofing through IPv6 trust subnets", () => {
  const fromSDK = createRequire(import.meta.resolve("@modelcontextprotocol/sdk/server/index.js"));
  const fromExpress = createRequire(fromSDK.resolve("express"));
  const proxyAddress = fromExpress("proxy-addr");

  for (const subnet of ["::ffff:10.0.0.0/8", "::/1"]) {
    for (const configuration of [subnet, [subnet, "127.0.0.1/32"]]) {
      const trust = proxyAddress.compile(configuration);
      for (const client of ["203.0.113.10", "::ffff:203.0.113.10"]) {
        assert.equal(trust(client), false, `${JSON.stringify(configuration)}: ${client}`);
      }
    }
  }
  for (const subnet of ["10.0.0.0/8", "::ffff:10.0.0.0/104"]) {
    const trust = proxyAddress.compile(subnet);
    for (const client of ["10.0.0.1", "::ffff:10.0.0.1"]) {
      assert.equal(trust(client), true, `${subnet}: ${client}`);
    }
    for (const client of ["203.0.113.10", "::ffff:203.0.113.10"]) {
      assert.equal(trust(client), false, `${subnet}: ${client}`);
    }
  }
});
