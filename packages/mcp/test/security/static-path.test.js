import assert from "node:assert/strict";
import fs from "node:fs/promises";
import { createRequire } from "node:module";
import os from "node:os";
import path from "node:path";
import test from "node:test";

test("MCP SDK static dependency cannot bypass protected paths through repeated decoding", async (t) => {
  const fromSDK = createRequire(import.meta.resolve("@modelcontextprotocol/sdk/server/index.js"));
  const { Hono } = fromSDK("hono");
  const { serveStatic } = fromSDK("@hono/node-server/serve-static");
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-static-path-"));
  t.after(() => fs.rm(directory, { recursive: true, force: true }));
  await fs.mkdir(path.join(directory, "private"));
  await fs.writeFile(path.join(directory, "private", "record.txt"), "protected fixture");
  await fs.writeFile(path.join(directory, "public.txt"), "public fixture");

  const app = new Hono();
  app.use("/private/*", (c) => c.text("Forbidden", 403));
  app.use("*", serveStatic({ root: directory }));
  const publicResponse = await app.request("http://localhost/public.txt");
  assert.equal(publicResponse.status, 200);
  assert.equal(await publicResponse.text(), "public fixture");
  const protectedResponse = await app.request("http://localhost/private/record.txt");
  assert.equal(protectedResponse.status, 403);
  await protectedResponse.text();

  for (const encoded of ["%%37%30rivate", "%70rivate", "%2570rivate", "%252570rivate", "private%2Frecord.txt"]) {
    const suffix = encoded.includes("record.txt") ? encoded : `${encoded}/record.txt`;
    const response = await app.request(`http://localhost/${suffix}`);
    assert.notEqual(response.status, 200, suffix);
    assert.notEqual(await response.text(), "protected fixture", suffix);
  }
});
