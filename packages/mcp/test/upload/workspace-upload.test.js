import assert from "node:assert/strict";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { pathToFileURL } from "node:url";
import test from "node:test";
import { ListRootsRequestSchema } from "@modelcontextprotocol/sdk/types.js";
import { openUploadSource } from "../../src/upload-source.js";
import { withGateway } from "../support/http-gateway.js";

async function directory(t) {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), "mcp-workspace-"));
  t.after(() => fs.rm(root, { force: true, recursive: true }));
  return fs.realpath(root);
}

function request(file, key) {
  return {
    name: "upload_connector_file",
    arguments: {
      target_ref: "ssh:1:1",
      action_name: "upload_file",
      local_path: file,
      input: { remote_path: "/srv/artifact", overwrite: false },
      reason: "Test workspace-local upload",
      idempotency_key: key,
    },
  };
}

function gateway() {
  const uploads = [];
  return {
    uploads,
    async handler(incoming, response) {
      response.setHeader("Content-Type", "application/json");
      if (incoming.method === "GET") {
        response.end(
          JSON.stringify({
            items: [
              {
                name: "upload_file",
                label: "Upload file",
                description: "Fixture",
                risk: "write",
                input_schema: { fields: [] },
                retry_policy: { class: "non_idempotent", guidance: "Inspect status" },
                max_input_bytes: 4096,
                max_upload_bytes: 1024,
              },
            ],
          }),
        );
      } else {
        const chunks = [];
        for await (const chunk of incoming) chunks.push(chunk);
        uploads.push(Buffer.concat(chunks));
        response.end(
          JSON.stringify({
            status: "approval_pending",
            request_id: uploads.length,
            target_ref: "ssh:1:1",
            connector_kind: "ssh",
            action_name: "upload_file",
            retry_policy: { class: "non_idempotent", guidance: "Inspect status" },
          }),
        );
      }
    },
  };
}

async function linuxOnly() {
  if (process.platform === "linux") return true;
  await assert.rejects(openUploadSource("/fixture/file", ["/fixture"], 10), /requires Linux/);
  return false;
}

test("packaged upload uses current client roots without configuration and rechecks workspace changes", { timeout: 15000 }, async (t) => {
  if (!(await linuxOnly())) return;
  const first = await directory(t);
  const second = await directory(t);
  for (const root of [first, second]) await fs.writeFile(path.join(root, "artifact"), "workspace-fixture");
  let roots = [{ uri: pathToFileURL(first).href }];
  const fixture = gateway();
  const client = await withGateway(
    t,
    fixture.handler,
    3000,
    "HTTP_TEST_TOKEN",
    "",
    {},
    {
      cwd: second,
      capabilities: { roots: { listChanged: true } },
      configure: (c) => c.setRequestHandler(ListRootsRequestSchema, () => ({ roots })),
    },
  );
  const accepted = await client.callTool(request(path.join(first, "artifact"), "first-workspace"));
  assert.notEqual(accepted.isError, true, JSON.stringify(accepted.content));
  assert.equal(JSON.parse(accepted.content[0].text).status, "approval_pending");
  const outside = await client.callTool(request(path.join(second, "artifact"), "outside-workspace"));
  assert.equal(outside.isError, true);
  assert.equal(fixture.uploads.length, 1);
  roots = [{ uri: pathToFileURL(second).href }];
  await client.sendRootsListChanged();
  const stale = await client.callTool(request(path.join(first, "artifact"), "stale-workspace"));
  assert.equal(stale.isError, true);
  const next = await client.callTool(request(path.join(second, "artifact"), "next-workspace"));
  assert.notEqual(next.isError, true, JSON.stringify(next.content));
  assert.equal(fixture.uploads.length, 2);
});

test("packaged upload falls back to launch cwd only when roots are unsupported", { timeout: 15000 }, async (t) => {
  if (!(await linuxOnly())) return;
  const root = await directory(t);
  const file = path.join(root, "artifact");
  await fs.writeFile(file, "cwd-fixture");
  const fixture = gateway();
  const client = await withGateway(t, fixture.handler, 3000, "HTTP_TEST_TOKEN", "", {}, { cwd: root });
  const result = await client.callTool(request(file, "cwd-workspace"));
  assert.notEqual(result.isError, true, JSON.stringify(result.content));
  assert.equal(fixture.uploads.length, 1);
  const disabled = await withGateway(t, fixture.handler, 3000, "HTTP_TEST_TOKEN", "", { AIPERMISSION_UPLOAD_ROOTS: "[]" }, { cwd: root });
  const denied = await disabled.callTool(request(file, "explicitly-disabled"));
  assert.equal(denied.isError, true);
  assert.match(denied.content[0].text, /uploads are disabled/);
  assert.equal(fixture.uploads.length, 1);
});

test("advertised empty or failing roots never authorize an otherwise valid launch cwd", { timeout: 15000 }, async (t) => {
  if (!(await linuxOnly())) return;
  const root = await directory(t);
  const file = path.join(root, "artifact");
  await fs.writeFile(file, "must-not-upload");
  const fixture = gateway();
  let failure = false;
  const client = await withGateway(
    t,
    fixture.handler,
    3000,
    "HTTP_TEST_TOKEN",
    "",
    {},
    {
      cwd: root,
      capabilities: { roots: {} },
      configure: (c) =>
        c.setRequestHandler(ListRootsRequestSchema, () => {
          if (failure) throw new Error("Fixture roots failure");
          return { roots: [] };
        }),
    },
  );
  for (const key of ["empty-workspace", "failing-workspace"]) {
    const result = await client.callTool(request(file, key));
    assert.equal(result.isError, true);
    failure = true;
  }
  assert.equal(fixture.uploads.length, 0);
});
