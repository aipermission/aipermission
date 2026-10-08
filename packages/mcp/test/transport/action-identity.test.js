import assert from "node:assert/strict";
import test from "node:test";
import { withGateway } from "../support/http-gateway.js";

test("packaged MCP canonicalizes structural identities without changing opaque input", { timeout: 10000 }, async (t) => {
  const posted = [];
  const client = await withGateway(t, async (request, response) => {
    let body = "";
    for await (const chunk of request) body += chunk;
    posted.push(JSON.parse(body));
    response.writeHead(200, { "Content-Type": "application/json" });
    response.end(
      JSON.stringify({
        status: "completed",
        request_id: 42,
        target_ref: "redis:1:1",
        connector_kind: "redis",
        action_name: "set_string",
        retry_policy: { class: "idempotent", guidance: "Reuse the same key." },
      }),
    );
  });
  for (const args of [
    { target_ref: " redis:1:1 ", action_name: "set_string" },
    { target_ref: "redis:1:1", action_name: "\tset_string\n" },
  ]) {
    const result = await client.callTool({
      name: "call_connector_action",
      arguments: {
        ...args,
        input: { key: " opaque key ", value: " value " },
        idempotency_key: "identity-smoke",
      },
    });
    assert.notEqual(result.isError, true);
    assert.equal(JSON.parse(result.content[0].text).request_id, 42);
  }
  assert.equal(posted.length, 2);
  for (const payload of posted) {
    assert.equal(payload.target_ref, "redis:1:1");
    assert.equal(payload.action_name, "set_string");
    assert.deepEqual(payload.input, { key: " opaque key ", value: " value " });
  }
});

test("packaged MCP rejects empty structural identities before dispatch", { timeout: 10000 }, async (t) => {
  let dispatched = false;
  const client = await withGateway(t, (_request, response) => {
    dispatched = true;
    response.end("{}");
  });
  for (const args of [
    { target_ref: " ", action_name: "set_string" },
    { target_ref: "redis:1:1", action_name: "\t\n" },
  ]) {
    const result = await client.callTool({ name: "call_connector_action", arguments: { ...args, idempotency_key: "identity-empty" } });
    assert.equal(result.isError, true);
  }
  assert.equal(dispatched, false);
});
