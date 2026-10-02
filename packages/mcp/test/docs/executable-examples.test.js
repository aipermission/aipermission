import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import { AjvJsonSchemaValidator } from "@modelcontextprotocol/sdk/validation/ajv";
import { withGateway } from "../support/http-gateway.js";
import { connectorActionResponse } from "../support/response-fixtures.js";

const approvedDocs = ["docs/api/rest-api.md", "docs/api/mcp-tools.md", "docs/setup/mcp-client-setup.md", "packages/mcp/README.md"];
const examples = [];
for (const file of approvedDocs) {
  const markdown = await readFile(new URL(`../../../../${file}`, import.meta.url), "utf8");
  for (const match of markdown.matchAll(/^```json\r?\n([\s\S]*?)^```\s*$/gm)) {
    // Other REST fences include JSON message streams, not single MCP envelopes.
    if (!/"action_name"\s*:/.test(match[1]) || !/"(?:target_ref|project_ref)"\s*:/.test(match[1])) continue;
    const value = JSON.parse(match[1]);
    if (!value.action_name || !(value.target_ref || value.project_ref)) continue;
    const line = markdown.slice(0, match.index).split("\n").length;
    examples.push({ file, line, value });
  }
}
const requests = examples.filter(({ value }) => !Object.hasOwn(value, "status"));
const responses = examples.filter(({ value }) => Object.hasOwn(value, "status"));
const download = requests.find(({ file, value }) => file === approvedDocs[0] && value.action_name === "start_file_download");

function label({ file, line }) {
  return `${file}:${line}`;
}

function actionResponse(args) {
  return {
    ...connectorActionResponse,
    target_ref: args.target_ref,
    connector_kind: args.target_ref.split(":")[0],
    action_name: args.action_name,
  };
}

async function recordingGateway(t, reply) {
  const dispatched = [];
  const client = await withGateway(
    t,
    async (request, response) => {
      const chunks = [];
      for await (const chunk of request) chunks.push(chunk);
      dispatched.push({
        method: request.method,
        url: request.url,
        authorization: request.headers.authorization,
        contentType: request.headers["content-type"],
        body: chunks.length ? JSON.parse(Buffer.concat(chunks).toString("utf8")) : undefined,
      });
      response.writeHead(200, { "Content-Type": "application/json" });
      response.end(JSON.stringify(reply(dispatched.length)));
    },
    2000,
  );
  return { client, dispatched };
}

async function actionValidator(client) {
  const { tools } = await client.listTools();
  const tool = tools.find(({ name }) => name === "call_connector_action");
  assert.ok(tool, "the packaged stdio server must publish call_connector_action");
  return new AjvJsonSchemaValidator().getValidator(tool.inputSchema);
}

function expectedDispatch(args) {
  return {
    method: "POST",
    url: "/api/mcp/connector-actions/call",
    authorization: "Bearer HTTP_TEST_TOKEN",
    contentType: "application/json",
    body: { ...args, input: args.input ?? {}, reason: args.reason ?? "" },
  };
}

test("approved docs retain the executable action examples", () => {
  assert.ok(download, "REST download request fence must not disappear");
  for (const name of ["exec", "check_mailbox", "list_objects", "read_console", "restart_console_session"]) {
    assert.ok(
      requests.some(({ file, value }) => file === approvedDocs[1] && value.action_name === name),
      name,
    );
  }
  for (const [action, status] of [
    ["exec", "completed"],
    ["list_objects", "completed"],
    ["exec", "approval_pending"],
    ["exec", "running"],
    ["exec", "blocked"],
  ]) {
    assert.ok(
      responses.some(({ value }) => value.action_name === action && value.status === status),
      `${action}: ${status}`,
    );
  }
});

for (const example of requests) {
  test(`packaged MCP dispatches documented request ${label(example)}`, { timeout: 10000 }, async (t) => {
    const args = example.value;
    const expected = actionResponse(args);
    const { client, dispatched } = await recordingGateway(t, () => expected);
    const validate = await actionValidator(client);
    const result = await client.callTool({ name: "call_connector_action", arguments: args });
    assert.notEqual(result.isError, true, JSON.stringify(result.content));
    const validation = validate(args);
    assert.equal(validation.valid, true, validation.errorMessage);
    assert.deepEqual(dispatched, [expectedDispatch(args)]);
    assert.deepEqual(JSON.parse(result.content[0].text), expected);
  });

  test(`packaged MCP refuses missing key from ${label(example)} before dispatch`, { timeout: 10000 }, async (t) => {
    const args = { ...example.value };
    delete args.idempotency_key;
    const { client, dispatched } = await recordingGateway(t, () => actionResponse(args));
    const validate = await actionValidator(client);
    assert.equal(validate(args).valid, false);
    const result = await client.callTool({ name: "call_connector_action", arguments: args });
    assert.equal(result.isError, true);
    assert.match(result.content[0].text, /idempotency_key/);
    assert.doesNotMatch(result.content[0].text, /outcome_unknown/);
    assert.deepEqual(dispatched, []);
  });
}

for (const example of responses) {
  test(`packaged MCP accepts documented response ${label(example)}`, { timeout: 10000 }, async (t) => {
    const expected = example.value;
    const requestID = expected.request_id ?? 1000;
    const request = requests.find(({ value }) => value.target_ref === expected.target_ref && value.action_name === expected.action_name);
    assert.ok(request, "a documented response must have a matching request example");
    const { client, dispatched } = await recordingGateway(t, () => expected);
    const called = await client.callTool({ name: "call_connector_action", arguments: request.value });
    assert.equal(called.isError === true, !["completed", "approval_pending", "running"].includes(expected.status));
    assert.deepEqual(JSON.parse(called.content[0].text), expected);
    const result = await client.callTool({ name: "get_connector_action_request", arguments: { request_id: requestID } });
    assert.notEqual(result.isError, true, JSON.stringify(result.content));
    assert.deepEqual(JSON.parse(result.content[0].text), expected);
    assert.deepEqual(dispatched, [
      expectedDispatch(request.value),
      {
        method: "GET",
        url: `/api/mcp/connector-action-requests/${requestID}`,
        authorization: "Bearer HTTP_TEST_TOKEN",
        contentType: undefined,
        body: undefined,
      },
    ]);
  });
}

test("documented download reconciles an unknown response only on explicit same-key submission", { timeout: 10000 }, async (t) => {
  assert.ok(download);
  const args = download.value;
  const expected = { ...actionResponse(args), replayed: true };
  const { client, dispatched } = await recordingGateway(t, (count) => (count === 1 ? {} : expected));
  const unknown = await client.callTool({ name: "call_connector_action", arguments: args });
  assert.equal(unknown.isError, true);
  const error = JSON.parse(unknown.content[0].text);
  assert.equal(error.status, "outcome_unknown");
  assert.equal(error.code, "gateway_response_contract_outcome_unknown");
  assert.equal(error.idempotency_key, args.idempotency_key);
  assert.equal(error.request_id, undefined, "the bridge must not invent a request ID");
  assert.deepEqual(dispatched, [expectedDispatch(args)], "the bridge must not automatically retry");

  const reconciled = await client.callTool({ name: "call_connector_action", arguments: args });
  assert.notEqual(reconciled.isError, true, JSON.stringify(reconciled.content));
  assert.deepEqual(JSON.parse(reconciled.content[0].text), expected);
  assert.deepEqual(dispatched, [expectedDispatch(args), expectedDispatch(args)]);
});
