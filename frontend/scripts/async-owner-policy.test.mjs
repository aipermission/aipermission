import assert from "node:assert/strict";
import test from "node:test";

import { isAsyncStateOwner } from "./async-owner-policy.mjs";

test("discovers request guards, sockets, abort controllers, timers, and generation guards", () => {
  for (const source of [
    "const requests = useRequestGuard('scope');",
    "const action = useAsyncAction();",
    "const requests = createRequestGuard('scope');",
    "const guard = createPollGenerationGuard();",
    "const controller = new AbortController();",
    "const socket = new WebSocket(url);",
    "window.setTimeout(load, 5000);",
    "setTimeout(async () => apiGet('/status'), 250);",
    "setTimeout(() => apiGet('/status'), 250);",
    "setInterval(refresh, 1000);",
    "++requestGeneration.current;",
    "requestGeneration.current = requestID;",
    "backupRecordsRequest.current = requestID;",
    "requests.begin('load');",
    "requestGuard.invalidate('load');",
  ]) {
    assert.equal(isAsyncStateOwner(source), true, source);
  }
});

test("does not classify unrelated state and method names as async ownership", () => {
  assert.equal(isAsyncStateOwner("model.begin(); revision += 1; const timeout = 5;"), false);
  assert.equal(isAsyncStateOwner("// setTimeout(load, 5000)"), false);
  assert.equal(isAsyncStateOwner("setTimeout(() => setOpen(false), 120)"), false);
});
