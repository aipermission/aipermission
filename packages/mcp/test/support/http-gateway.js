import http from "node:http";
import path from "node:path";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StdioClientTransport } from "@modelcontextprotocol/sdk/client/stdio.js";

export async function withGateway(t, handler, timeout = 100, token = "HTTP_TEST_TOKEN", userinfo = "") {
  const gateway = http.createServer(handler);
  await new Promise((resolve) => gateway.listen(0, "127.0.0.1", resolve));
  t.after(() => {
    gateway.closeAllConnections();
    return new Promise((resolve) => gateway.close(resolve));
  });
  return clientForGatewayURL(t, `http://${userinfo}127.0.0.1:${gateway.address().port}`, timeout, token);
}

export async function clientForGatewayURL(t, gatewayURL, timeout = 100, token = "HTTP_TEST_TOKEN") {
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
