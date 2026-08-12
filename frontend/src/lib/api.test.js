import assert from "node:assert/strict";
import test from "node:test";

import { apiPost } from "./api.js";

test("local connector action retries retain idempotency after uncertain transport failure", async () => {
  const originalFetch = globalThis.fetch;
  const bodies = [];
  let calls = 0;
  globalThis.fetch = async (_url, options) => {
    bodies.push(JSON.parse(options.body));
    calls += 1;
    if (calls === 1) throw new TypeError("network disconnected");
    return response({ ok: true });
  };
  try {
    const first = { target_ref: "fixture:1:1", action_name: "inspect", input: { b: 2, a: 1 }, reason: "test" };
    await assert.rejects(() => apiPost("/api/connector-actions/local-run", first), /network disconnected/);
    const reordered = { reason: "test", input: { a: 1, b: 2 }, action_name: "inspect", target_ref: "fixture:1:1" };
    await apiPost("/api/connector-actions/local-run", reordered);
    await apiPost("/api/connector-actions/local-run", first);

    assert.equal(bodies[0].idempotency_key, bodies[1].idempotency_key);
    assert.notEqual(bodies[1].idempotency_key, bodies[2].idempotency_key);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("local connector action retries retain idempotency after server failures", async () => {
  const originalFetch = globalThis.fetch;
  const keys = [];
  let calls = 0;
  globalThis.fetch = async (_url, options) => {
    keys.push(JSON.parse(options.body).idempotency_key);
    calls += 1;
    return calls === 1 ? response({ error: "gateway failed" }, 502) : response({ ok: true });
  };
  try {
    const body = { target_ref: "fixture:1:1", action_name: "inspect", input: {}, reason: "test" };
    await assert.rejects(() => apiPost("/api/connector-actions/local-run", body), /gateway failed/);
    await apiPost("/api/connector-actions/local-run", body);
    assert.equal(keys[0], keys[1]);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("local connector action retry keys expire after a bounded window", async () => {
  const originalFetch = globalThis.fetch;
  const originalNow = Date.now;
  const keys = [];
  let now = 1000;
  Date.now = () => now;
  globalThis.fetch = async (_url, options) => {
    keys.push(JSON.parse(options.body).idempotency_key);
    throw new TypeError("network disconnected");
  };
  const body = { target_ref: "fixture:expiry", action_name: "inspect", input: {}, reason: "test" };
  try {
    await assert.rejects(() => apiPost("/api/connector-actions/local-run", body));
    now += 6 * 60 * 1000;
    await assert.rejects(() => apiPost("/api/connector-actions/local-run", body));
    assert.notEqual(keys[0], keys[1]);
  } finally {
    Date.now = originalNow;
    globalThis.fetch = originalFetch;
  }
});

test("local connector action retry keys evict the oldest uncertain request", async () => {
  const originalFetch = globalThis.fetch;
  const firstKeys = [];
  globalThis.fetch = async (_url, options) => {
    firstKeys.push(JSON.parse(options.body).idempotency_key);
    throw new TypeError("network disconnected");
  };
  const first = { target_ref: "fixture:lru:0", action_name: "inspect", input: {}, reason: "test" };
  try {
    await assert.rejects(() => apiPost("/api/connector-actions/local-run", first));
    for (let index = 1; index <= 128; index += 1) {
      const body = { ...first, target_ref: `fixture:lru:${index}` };
      await assert.rejects(() => apiPost("/api/connector-actions/local-run", body));
    }
    await assert.rejects(() => apiPost("/api/connector-actions/local-run", first));
    assert.notEqual(firstKeys[0], firstKeys.at(-1));
  } finally {
    globalThis.fetch = originalFetch;
  }
});

function response(body, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    async text() {
      return JSON.stringify(body);
    },
  };
}
