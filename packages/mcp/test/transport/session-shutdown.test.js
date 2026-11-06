import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { once } from "node:events";
import http from "node:http";
import path from "node:path";
import { createInterface } from "node:readline";
import test from "node:test";

test("stdin EOF closes the MCP session and its active gateway connection", { timeout: 10000 }, async (t) => {
  const received = Promise.withResolvers();
  const disconnected = Promise.withResolvers();
  const gateway = http.createServer((_request, response) => {
    response.once("close", disconnected.resolve);
    received.resolve();
  });
  await new Promise((resolve) => gateway.listen(0, "127.0.0.1", resolve));
  t.after(() => {
    gateway.closeAllConnections();
    return new Promise((resolve) => gateway.close(resolve));
  });
  const child = spawn(process.execPath, [path.resolve("dist/server.js")], {
    env: {
      ...process.env,
      AIPERMISSION_API_URL: `http://127.0.0.1:${gateway.address().port}`,
      AIPERMISSION_API_TOKEN: "shutdown-fixture-token",
    },
    stdio: ["pipe", "pipe", "pipe"],
  });
  t.after(() => {
    if (child.exitCode === null) child.kill();
  });
  const exited = once(child, "exit");
  const lines = createInterface({ input: child.stdout });
  t.after(() => lines.close());
  const initialized = once(lines, "line");
  child.stdin.write(
    JSON.stringify({
      jsonrpc: "2.0",
      id: 1,
      method: "initialize",
      params: { protocolVersion: "2025-03-26", capabilities: {}, clientInfo: { name: "shutdown-test", version: "1" } },
    }) + "\n",
  );
  const [line] = await initialized;
  assert.equal(JSON.parse(line).id, 1);
  child.stdin.write(JSON.stringify({ jsonrpc: "2.0", method: "notifications/initialized" }) + "\n");
  child.stdin.write(
    JSON.stringify({ jsonrpc: "2.0", id: 2, method: "tools/call", params: { name: "list_connector_targets", arguments: {} } }) + "\n",
  );
  await received.promise;
  child.stdin.end();
  await disconnected.promise;
  const [code, signal] = await exited;
  assert.equal(code, 0);
  assert.equal(signal, null);
});
