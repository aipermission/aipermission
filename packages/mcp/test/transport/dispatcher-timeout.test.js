import assert from "node:assert/strict";
import { setImmediate } from "node:timers/promises";
import { createRequire } from "node:module";
import http from "node:http";
import test from "node:test";
import { Agent, fetch } from "undici";
import { createHTTPDispatcher, parseHTTPTimeout } from "../../src/config.js";

// The pinned library exposes this clock for tests. Never accelerate wall time
// or production timers; keep real sockets and actual protocol timeout handling.
const require = createRequire(import.meta.url);
const clock = require("undici/lib/util/timers.js");

async function delayedReply(dispatcher, phase) {
  clock.reset();
  const received = Promise.withResolvers();
  const gateway = http.createServer((_request, response) => {
    if (phase === "body") {
      response.writeHead(200);
      response.write("first");
    }
    received.resolve(response);
  });
  await new Promise((resolve) => gateway.listen(0, "127.0.0.1", resolve));
  try {
    const response = fetch(`http://127.0.0.1:${gateway.address().port}`, { dispatcher, signal: AbortSignal.timeout(5000) });
    const outcome = response
      .then((item) => item.text())
      .then(
        (text) => ({ text }),
        (error) => ({ error }),
      );
    const reply = await received.promise;
    if (phase === "body") await response;
    await setImmediate();
    clock.tick();
    clock.tick(301_000);
    await setImmediate();
    reply.end("last");
    return await outcome;
  } finally {
    await dispatcher.destroy();
    gateway.closeAllConnections();
    await new Promise((resolve) => gateway.close(resolve));
    clock.reset();
  }
}

for (const phase of ["headers", "body"]) {
  test(`HTTP ${phase} budget outlives the old 300-second transport cap`, { timeout: 10000 }, async () => {
    const old = await delayedReply(new Agent(), phase);
    assert.equal(old.error?.cause?.code, phase === "headers" ? "UND_ERR_HEADERS_TIMEOUT" : "UND_ERR_BODY_TIMEOUT");
    const current = await delayedReply(createHTTPDispatcher(parseHTTPTimeout()), phase);
    assert.equal(current.error, undefined);
    assert.equal(current.text, phase === "headers" ? "last" : "firstlast");
  });
}

test("HTTP dispatcher refuses nonfinite or out-of-contract budgets", () => {
  for (const value of [0, -1, Infinity, NaN, 600001]) {
    assert.throws(() => createHTTPDispatcher(value), /AIPERMISSION_HTTP_TIMEOUT_MS/);
  }
});
