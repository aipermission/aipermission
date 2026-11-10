import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { openUploadSource, parseUploadRoots } from "../../src/upload-source.js";
import { readActionExamples } from "../support/doc-action-examples.js";
import { withGateway } from "../support/http-gateway.js";

async function uploadDirectory(t) {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-upload-test-"));
  t.after(() => fs.rm(directory, { recursive: true, force: true }));
  return fs.realpath(directory);
}

test("explicit upload roots do not accept broad or ambiguous paths", async () => {
  assert.deepEqual(parseUploadRoots(), []);
  for (const roots of ["/home", '["/"]', '["/tmp/../etc"]', '["/tmp/"]', '["relative"]', "{}"]) {
    assert.throws(() => parseUploadRoots(roots), /AIPERMISSION_UPLOAD_ROOTS/);
  }
  await assert.rejects(openUploadSource("/tmp/file", [], 10), /uploads are disabled/);
});

test("upload source refuses symlinked parents, file symlinks, hard links and oversized files", async (t) => {
  if (process.platform !== "linux") {
    await assert.rejects(openUploadSource("/fixture/file", ["/fixture"], 10), /requires Linux/);
    return;
  }
  const root = await uploadDirectory(t);
  const outside = await uploadDirectory(t);
  const source = path.join(outside, "file");
  await fs.writeFile(source, "fixture-data");
  await fs.symlink(outside, path.join(root, "redirected"));
  await fs.symlink(source, path.join(root, "symlink"));
  await fs.link(source, path.join(root, "hardlink"));
  await fs.copyFile(source, path.join(root, "oversize"));
  await assert.rejects(openUploadSource(source, [root], 100), /inside an approved/);
  for (const file of ["redirected/file", "symlink"]) {
    await assert.rejects(openUploadSource(path.join(root, file), [root], 100), /Symlinks and inaccessible files are not allowed/);
  }
  await assert.rejects(openUploadSource(path.join(root, "hardlink"), [root], 100), /stable, single-link regular file/);
  await assert.rejects(openUploadSource(path.join(root, "oversize"), [root], 10), /exceeds the connector upload limit/);
  const allowed = await openUploadSource(path.join(root, "oversize"), [root], 100);
  try {
    assert.equal(await allowed.checksum(), createHash("sha256").update("fixture-data").digest("hex"));
  } finally {
    await allowed.close();
  }
});

test("descriptor paths cannot cross a mounted subtree of an approved root", async () => {
  if (process.platform !== "linux") {
    await assert.rejects(openUploadSource("/fixture/file", ["/fixture"], 10), /requires Linux/);
    return;
  }
  // Linux mounts shared memory separately from /dev.
  await assert.rejects(openUploadSource("/dev/shm/not-an-upload", ["/dev"], 1024), /must not cross mounts/);
});

test("explicit nested mount roots work in either order without authorizing unlisted mounts", async (t) => {
  if (process.platform !== "linux") return;
  const directory = await fs.mkdtemp("/dev/shm/aipermission-upload-roots-");
  t.after(() => fs.rm(directory, { recursive: true, force: true }));
  const file = path.join(directory, "artifact");
  await fs.writeFile(file, "fixture-data");
  for (const roots of [
    ["/dev", "/dev/shm"],
    ["/dev/shm", "/dev"],
  ]) {
    const original = [...roots];
    const source = await openUploadSource(file, roots, 1024);
    try {
      assert.equal(source.size, 12);
      assert.deepEqual(roots, original);
      await source.checksum();
    } finally {
      await source.close();
    }
  }
  await assert.rejects(openUploadSource(file, ["/dev"], 1024), /must not cross mounts/);
});

test("a changed local file cannot finish the staged upload stream", async (t) => {
  if (process.platform !== "linux") {
    await assert.rejects(openUploadSource("/fixture/file", ["/fixture"], 10), /requires Linux/);
    return;
  }
  const root = await uploadDirectory(t);
  const file = path.join(root, "artifact");
  await fs.writeFile(file, "original");
  const source = await openUploadSource(file, [root], 100);
  t.after(() => source.close());
  await source.checksum();
  await fs.rename(file, path.join(root, "renamed"));
  await assert.rejects(source.checksum(), /file changed/);
});

test("packaged MCP streams binary bytes outside JSON and returns approval separately", { timeout: 15000 }, async (t) => {
  const examples = await readActionExamples("docs/api/mcp-tools.md");
  const documented = examples.find(({ value }) => value.action_name === "upload_file" && value.local_path)?.value;
  assert.ok(documented, "the streaming upload example must remain executable");
  if (process.platform !== "linux") {
    await assert.rejects(openUploadSource("/fixture/file", ["/fixture"], 10), /requires Linux/);
    return;
  }
  const root = await uploadDirectory(t);
  const localPath = path.join(root, "test-artifact.bin");
  const bytes = Buffer.alloc((2 << 20) + 7, 0xa7);
  await fs.writeFile(localPath, bytes);
  let upload;
  const client = await withGateway(
    t,
    async (request, response) => {
      response.setHeader("Content-Type", "application/json");
      if (request.method === "GET") {
        response.end(
          JSON.stringify({
            items: [
              {
                name: "upload_file",
                label: "Upload file",
                description: "Fixture",
                risk: "write",
                input_schema: { fields: [] },
                retry_policy: { class: "non_idempotent", guidance: "Inspect status." },
                max_input_bytes: 4096,
                max_upload_bytes: 4 << 20,
              },
            ],
          }),
        );
        return;
      }
      const chunks = [];
      for await (const chunk of request) chunks.push(chunk);
      upload = { url: request.url, headers: request.headers, body: Buffer.concat(chunks), chunks: chunks.length };
      response.end(
        JSON.stringify({
          status: "approval_pending",
          request_id: 7,
          target_ref: "ssh:1:1",
          connector_kind: "ssh",
          action_name: "upload_file",
          retry_policy: { class: "non_idempotent", guidance: "Inspect status." },
        }),
      );
    },
    2000,
    "HTTP_TEST_TOKEN",
    "",
    { AIPERMISSION_UPLOAD_ROOTS: JSON.stringify([root]) },
  );
  const result = await client.callTool({
    name: "upload_connector_file",
    arguments: {
      ...documented,
      local_path: localPath,
    },
  });
  assert.notEqual(result.isError, true, JSON.stringify(result.content));
  assert.equal(JSON.parse(result.content[0].text).status, "approval_pending");
  assert.equal(upload.url, "/api/mcp/connector-actions/upload");
  assert.equal(upload.headers["transfer-encoding"], "chunked");
  assert.ok(upload.chunks > 1);
  const firstPart = upload.body.indexOf("\r\n\r\n") + 4;
  const endOfMetadata = upload.body.indexOf("\r\n--", firstPart);
  const metadata = JSON.parse(upload.body.subarray(firstPart, endOfMetadata));
  assert.deepEqual(metadata.manifest, {
    file_name: path.basename(localPath),
    remote_path: documented.input.remote_path,
    size_bytes: bytes.length,
    checksum_sha256: createHash("sha256").update(bytes).digest("hex"),
    overwrite: false,
  });
  assert.ok(!upload.body.includes(localPath));
  const firstByte = upload.body.indexOf("\r\n\r\n", endOfMetadata) + 4;
  assert.ok(upload.body.subarray(firstByte, firstByte + bytes.length).equals(bytes));
});

test("new MCP clients can discover an old gateway without attempting an upload", { timeout: 10000 }, async (t) => {
  const root = await uploadDirectory(t);
  let posts = 0;
  const client = await withGateway(
    t,
    (request, response) => {
      if (request.method === "POST") posts++;
      response.setHeader("Content-Type", "application/json");
      response.end(JSON.stringify({ items: [] }));
    },
    2000,
    "HTTP_TEST_TOKEN",
    "",
    { AIPERMISSION_UPLOAD_ROOTS: JSON.stringify([root]) },
  );
  const result = await client.callTool({
    name: "upload_connector_file",
    arguments: {
      target_ref: "ssh:1:1",
      action_name: "upload_file",
      local_path: path.join(root, "not-present"),
      input: { remote_path: "/srv/artifact.bin" },
      reason: "Verify compatibility",
      idempotency_key: "old-gateway",
    },
  });
  if (process.platform === "win32") {
    assert.match(result.content[0].text, /canonical absolute directories/);
  } else {
    assert.match(result.content[0].text, /does not support streamed uploads/);
  }
  assert.equal(posts, 0);
});
