import assert from "node:assert/strict";
import http from "node:http";
import path from "node:path";
import test from "node:test";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StdioClientTransport } from "@modelcontextprotocol/sdk/client/stdio.js";

async function withGateway(t, handler, timeout = 100, token = "HTTP_TEST_TOKEN", userinfo = "") {
  const gateway = http.createServer(handler);
  await new Promise((resolve) => gateway.listen(0, "127.0.0.1", resolve));
  t.after(() => {
    gateway.closeAllConnections();
    return new Promise((resolve) => gateway.close(resolve));
  });
  return clientForGatewayURL(t, `http://${userinfo}127.0.0.1:${gateway.address().port}`, timeout, token);
}

async function clientForGatewayURL(t, gatewayURL, timeout = 100, token = "HTTP_TEST_TOKEN") {
  const transport = new StdioClientTransport({
    command: process.execPath,
    args: [path.resolve("dist/cli.js")],
    env: {
      NODE_ENV: "production",
      AIPERMISSION_API_URL: gatewayURL,
      AIPERMISSION_API_TOKEN: token,
      AIPERMISSION_HTTP_TIMEOUT_MS: String(timeout),
    },
    stderr: "pipe",
  });
  const client = new Client({ name: "http-boundary-test", version: "1.0.0" });
  t.after(() => client.close());
  await client.connect(transport, { timeout: 5000 });
  return client;
}

test("mutation connection refusal is classified before dispatch", { timeout: 10000 }, async (t) => {
  const unavailable = http.createServer();
  await new Promise((resolve) => unavailable.listen(0, "127.0.0.1", resolve));
  const port = unavailable.address().port;
  await new Promise((resolve) => unavailable.close(resolve));
  const client = await clientForGatewayURL(t, `http://127.0.0.1:${port}`, 2000);

  const result = await client.callTool({
    name: "call_connector_action",
    arguments: {
      target_ref: "redis:1:1",
      action_name: "set_string",
      input: {},
      reason: "predispatch fixture",
      idempotency_key: "not-dispatched",
    },
  });
  const data = JSON.parse(result.content[0].text);
  assert.equal(result.isError, true);
  assert.equal(data.status, "error");
  assert.match(data.error, /failed before request dispatch/);
  assert.equal(data.code, undefined);
  assert.equal(data.idempotency_key, undefined);

  const vaultResult = await client.callTool({ name: "cancel_vault_action_request", arguments: { request_id: 12 } });
  const vaultData = JSON.parse(vaultResult.content[0].text);
  assert.equal(vaultResult.isError, true);
  assert.equal(vaultData.status, "error");
  assert.match(vaultData.error, /failed before request dispatch/);
  assert.equal(vaultData.code, undefined);
});

for (const mode of ["headers", "body", "chunked"]) {
  test(`packaged MCP deadline covers delayed ${mode}`, { timeout: 10000 }, async (t) => {
    let timer;
    t.after(() => clearTimeout(timer));
    const client = await withGateway(t, (_request, response) => {
      if (mode !== "headers") {
        response.writeHead(200, { "Content-Type": "application/json" });
        response.flushHeaders();
      }
      if (mode === "chunked") response.write('{"targets":');
      timer = setTimeout(() => response.end(mode === "chunked" ? "[]}" : '{"targets":[]}'), 500);
    });
    const result = await client.callTool({ name: "list_connector_targets", arguments: {} });
    assert.equal(result.isError, true);
    assert.match(JSON.parse(result.content[0].text).error, /timed out after 100ms/);
  });
}

test("packaged MCP accepts a timely streamed body", { timeout: 10000 }, async (t) => {
  const client = await withGateway(
    t,
    (_request, response) => {
      response.writeHead(200, { "Content-Type": "application/json" });
      response.write('{"targets":');
      response.end("[]}");
    },
    2000,
  );
  const result = await client.callTool({ name: "list_connector_targets", arguments: {} });
  assert.notEqual(result.isError, true);
  assert.deepEqual(JSON.parse(result.content[0].text), { targets: [] });
});

test("one deadline spans delayed headers and continuing chunks without retry", { timeout: 10000 }, async (t) => {
  let requests = 0;
  let closed;
  const responseClosed = new Promise((resolve) => {
    closed = resolve;
  });
  let started;
  let closedAt;
  let headersSent = false;
  let chunksSent = 0;
  const client = await withGateway(
    t,
    (_request, response) => {
      requests++;
      if (requests > 1) {
        response.end('{"targets":[]}');
        return;
      }
      started = performance.now();
      const headerTimer = setTimeout(() => {
        response.writeHead(200, { "Content-Type": "application/json" });
        response.write('{"targets":[');
        headersSent = true;
      }, 180);
      const chunkTimer = setInterval(() => {
        if (response.headersSent) {
          response.write(" ");
          chunksSent++;
        }
      }, 40);
      response.on("close", () => {
        clearTimeout(headerTimer);
        clearInterval(chunkTimer);
        closedAt = performance.now();
        closed();
      });
    },
    300,
  );
  const result = await client.callTool({ name: "list_connector_targets", arguments: {} });
  assert.equal(result.isError, true);
  assert.match(JSON.parse(result.content[0].text).error, /timed out after 300ms/);
  await responseClosed;
  assert.equal(headersSent, true);
  assert.ok(chunksSent > 0);
  assert.ok(closedAt - started < 450, `request lasted ${closedAt - started}ms`);
  assert.equal(requests, 1);
  const next = await client.callTool({ name: "list_connector_targets", arguments: {} });
  assert.notEqual(next.isError, true);
  assert.equal(requests, 2);
});

test("timely gateway errors retain their metadata", { timeout: 10000 }, async (t) => {
  const client = await withGateway(
    t,
    (_request, response) => {
      response.writeHead(409, { "Content-Type": "application/json" });
      response.end(
        JSON.stringify({
          error: "reconcile first",
          code: "outcome_unknown",
          status: "outcome_unknown",
          request_id: 42,
          assistant_hint: "Inspect the original request.",
          retry_after_seconds: 2,
        }),
      );
    },
    2000,
  );
  const result = await client.callTool({ name: "list_connector_targets", arguments: {} });
  const data = JSON.parse(result.content[0].text);
  assert.equal(result.isError, true);
  assert.deepEqual(data, {
    error: "reconcile first",
    code: "outcome_unknown",
    status: "outcome_unknown",
    request_id: 42,
    assistant_hint: "Inspect the original request.",
    retry_after_seconds: 2,
  });
});

test("truncated response body is not misclassified as timeout", { timeout: 10000 }, async (t) => {
  let timer;
  t.after(() => clearTimeout(timer));
  const client = await withGateway(
    t,
    (_request, response) => {
      response.writeHead(200, { "Content-Type": "application/json", "Content-Length": "200" });
      const socket = response.socket;
      response.flushHeaders();
      response.write("{}");
      timer = setTimeout(() => socket.destroy(), 50);
    },
    2000,
  );
  const result = await client.callTool({ name: "list_connector_targets", arguments: {} });
  assert.equal(result.isError, true);
  assert.match(JSON.parse(result.content[0].text).error, /Gateway response unavailable or invalid/);
});

test("lost mutation response preserves uncertain outcome and original identity", { timeout: 10000 }, async (t) => {
  const received = [];
  const client = await withGateway(
    t,
    async (request, response) => {
      let body = "";
      for await (const chunk of request) body += chunk;
      received.push(JSON.parse(body));
      response.destroy();
    },
    2000,
  );
  const result = await client.callTool({
    name: "call_connector_action",
    arguments: {
      target_ref: "redis:1:1",
      action_name: "set_string",
      input: { key: "fixture", value: "value" },
      reason: "transport fixture",
      idempotency_key: "original-request",
    },
  });
  const data = JSON.parse(result.content[0].text);
  assert.equal(data.status, "outcome_unknown");
  assert.equal(data.idempotency_key, "original-request");
  assert.equal(data.request_id, undefined);
  assert.match(data.assistant_hint, /same idempotency key/);
  assert.equal(received.length, 1);
  assert.equal(received[0].idempotency_key, "original-request");
});

for (const mode of ["timeout", "partial-body", "malformed-json", "rejected", "gateway-unknown", "invalid-input"]) {
  test(`mutation transport distinguishes ${mode}`, { timeout: 10000 }, async (t) => {
    let requests = 0;
    let timer;
    t.after(() => clearTimeout(timer));
    const client = await withGateway(t, async (request, response) => {
      requests++;
      for await (const chunk of request) {
        void chunk;
      }
      if (mode === "timeout") return;
      if (mode === "malformed-json") {
        response.end('{"status": "SECRET_MARKER"');
        return;
      }
      if (mode === "partial-body") {
        response.writeHead(200, { "Content-Type": "application/json" });
        response.write('{"status":');
        timer = setTimeout(() => response.destroy(), 30);
        return;
      }
      response.writeHead(409, { "Content-Type": "application/json" });
      response.end(
        JSON.stringify({
          status: mode === "rejected" ? "blocked" : "outcome_unknown",
          error: "gateway result",
          request_id: 12,
          assistant_hint: "Inspect request 12.",
        }),
      );
    });
    const result = await client.callTool({
      name: "call_connector_action",
      arguments: {
        target_ref: "redis:1:1",
        action_name: "set_string",
        input: {},
        reason: "fixture",
        idempotency_key: mode === "invalid-input" ? "" : "same-request",
      },
    });
    assert.equal(result.isError, true);
    if (mode === "invalid-input") {
      assert.equal(requests, 0);
      assert.doesNotMatch(result.content[0].text, /gateway_transport_outcome_unknown/);
      return;
    }
    const data = JSON.parse(result.content[0].text);
    assert.equal(requests, 1);
    if (mode === "timeout" || mode === "partial-body" || mode === "malformed-json") {
      assert.equal(data.status, "outcome_unknown");
      assert.equal(data.idempotency_key, "same-request");
      assert.equal(data.request_id, undefined);
      assert.equal(data.code, "gateway_transport_outcome_unknown");
      assert.doesNotMatch(result.content[0].text, /SECRET_MARKER/);
    } else {
      assert.equal(data.status, mode === "rejected" ? "blocked" : "outcome_unknown");
      assert.equal(data.request_id, 12);
      assert.equal(data.assistant_hint, "Inspect request 12.");
      assert.notEqual(data.code, "gateway_transport_outcome_unknown");
    }
  });
}

test("invalid token headers fail before sending and never expose their value", { timeout: 10000 }, async (t) => {
  let requests = 0;
  const client = await withGateway(
    t,
    (_request, response) => {
      requests++;
      response.end("{}");
    },
    2000,
    "HEADER_SECRET\nINVALID",
  );
  const result = await client.callTool({
    name: "call_connector_action",
    arguments: {
      target_ref: "redis:1:1",
      action_name: "set_string",
      input: {},
      idempotency_key: "request-1",
    },
  });
  const data = JSON.parse(result.content[0].text);
  assert.equal(data.status, "error");
  assert.equal(requests, 0);
  assert.doesNotMatch(result.content[0].text, /HEADER_SECRET|INVALID/);
  assert.equal(data.idempotency_key, undefined);
});

test("URL credentials fail locally without claiming dispatch or leaking values", { timeout: 10000 }, async (t) => {
  let requests = 0;
  const client = await withGateway(
    t,
    (_request, response) => {
      requests++;
      response.end("{}");
    },
    2000,
    "HTTP_TEST_TOKEN",
    "fixture:URL_SECRET@",
  );
  const result = await client.callTool({
    name: "call_connector_action",
    arguments: {
      target_ref: "redis:1:1",
      action_name: "set_string",
      input: {},
      idempotency_key: "request-1",
    },
  });
  const data = JSON.parse(result.content[0].text);
  assert.equal(data.status, "error");
  assert.equal(requests, 0);
  assert.doesNotMatch(result.content[0].text, /URL_SECRET|outcome_unknown/);
});

test("Vault mutations and cancellation share transport uncertainty", { timeout: 10000 }, async (t) => {
  const client = await withGateway(
    t,
    async (request, response) => {
      for await (const chunk of request) {
        void chunk;
      }
      response.destroy();
    },
    2000,
  );
  const generated = await client.callTool({
    name: "call_vault_action",
    arguments: {
      project_ref: "my-project",
      action_name: "generate_item",
      input: {},
      reason: "fixture",
      idempotency_key: "vault-original",
    },
  });
  const generationError = JSON.parse(generated.content[0].text);
  assert.equal(generationError.status, "outcome_unknown");
  assert.equal(generationError.idempotency_key, "vault-original");
  const canceled = await client.callTool({ name: "cancel_vault_action_request", arguments: { request_id: 12 } });
  const cancelError = JSON.parse(canceled.content[0].text);
  assert.equal(cancelError.status, "outcome_unknown");
  assert.equal(cancelError.idempotency_key, undefined);
  assert.equal(cancelError.request_id, undefined);
  assert.match(cancelError.assistant_hint, /Inspect the original request status/);
});
