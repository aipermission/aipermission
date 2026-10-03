import assert from "node:assert/strict";
import http from "node:http";
import test from "node:test";
import { clientForGatewayURL } from "../support/http-gateway.js";

for (const marker of ["?", "#", "?#", "/#"]) {
  test(`packaged MCP refuses bare ${marker} origin before dispatch`, { timeout: 10000 }, async (t) => {
    let calls = 0;
    const gateway = http.createServer((_request, response) => {
      calls += 1;
      response.writeHead(200, { "Content-Type": "application/json" });
      response.end("[]");
    });
    await new Promise((resolve) => gateway.listen(0, "127.0.0.1", resolve));
    t.after(() => {
      gateway.closeAllConnections();
      return new Promise((resolve) => gateway.close(resolve));
    });
    const client = await clientForGatewayURL(t, `http://127.0.0.1:${gateway.address().port}${marker}`, 2000);
    const result = await client.callTool({ name: "list_connector_targets", arguments: {} });
    assert.equal(result.isError, true);
    assert.match(JSON.parse(result.content[0].text).error, /Invalid local gateway URL/);
    assert.equal(calls, 0);
  });
}
