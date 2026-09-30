import assert from "node:assert/strict";
import test from "node:test";
import { withGateway } from "../support/http-gateway.js";

test("packaged MCP preserves a withheld connector replay and its polling handle", { timeout: 10000 }, async (t) => {
  const idempotencyKeys = new Set();
  const posted = [];
  let dispatches = 0;
  const withheld = {
    status: "completed",
    request_id: 71,
    target_ref: "",
    connector_kind: "",
    action_name: "set_string",
    retry_policy: { class: "non_idempotent", guidance: "Reconcile the original request." },
    output_withheld: true,
    assistant_hint: "Output is withheld because delivery permission was lost.",
  };
  const client = await withGateway(
    t,
    async (request, response) => {
      response.writeHead(200, { "Content-Type": "application/json" });
      if (request.method === "POST" && request.url === "/api/mcp/connector-actions/call") {
        let body = "";
        for await (const chunk of request) body += chunk;
        const input = JSON.parse(body);
        posted.push(input);
        const replayed = idempotencyKeys.has(input.idempotency_key);
        if (!replayed) {
          idempotencyKeys.add(input.idempotency_key);
          dispatches++;
        }
        response.end(JSON.stringify({ ...withheld, replayed }));
        return;
      }
      assert.equal(request.url, "/api/mcp/connector-action-requests/71");
      response.end(JSON.stringify(withheld));
    },
    2000,
  );
  const args = {
    target_ref: "redis:1:1",
    action_name: "set_string",
    input: { key: "synthetic-fixture", value: "synthetic-value" },
    reason: "Verify safe withheld delivery.",
    idempotency_key: "withheld-connector-fixture",
  };
  for (const replayed of [false, true]) {
    const result = await client.callTool({ name: "call_connector_action", arguments: args });
    assert.notEqual(result.isError, true);
    assert.deepEqual(JSON.parse(result.content[0].text), { ...withheld, replayed });
  }
  const polled = await client.callTool({ name: "get_connector_action_request", arguments: { request_id: 71 } });
  assert.notEqual(polled.isError, true);
  assert.deepEqual(JSON.parse(polled.content[0].text), withheld);
  assert.equal(dispatches, 1);
  assert.deepEqual(posted, [args, args]);
});
