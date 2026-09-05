import assert from "node:assert/strict";
import http from "node:http";
import path from "node:path";
import test from "node:test";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StdioClientTransport } from "@modelcontextprotocol/sdk/client/stdio.js";

async function withGateway(t, handler, timeout = 100) {
  const gateway = http.createServer(handler);
  await new Promise((resolve) => gateway.listen(0, "127.0.0.1", resolve));
  t.after(() => {
    gateway.closeAllConnections();
    return new Promise((resolve) => gateway.close(resolve));
  });
  const transport = new StdioClientTransport({
    command: process.execPath,
    args: [path.resolve("dist/cli.js")],
    env: {
      NODE_ENV: "production",
      AIPERMISSION_API_URL: `http://127.0.0.1:${gateway.address().port}`,
      AIPERMISSION_API_TOKEN: "HTTP_TEST_TOKEN",
      AIPERMISSION_HTTP_TIMEOUT_MS: String(timeout),
    },
    stderr: "pipe",
  });
  const client = new Client({ name: "http-boundary-test", version: "1.0.0" });
  t.after(() => client.close());
  await client.connect(transport, { timeout: 5000 });
  return client;
}

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
  assert.match(JSON.parse(result.content[0].text).error, /terminated/);
});
