import assert from "node:assert/strict";
import test from "node:test";
import { formatDockerLogLine, resourceKey, resourceSearchValues, resourceTone, summarizePorts } from "./helpers.js";

test("Docker log formatting keeps structured messages readable", () => {
  const line = '2026-08-11T00:00:00Z {"Level":"Warning","Message":"Retrying","Properties":{"attempt":2}}';
  const formatted = formatDockerLogLine(line);
  assert.match(formatted, /2026-08-11T00:00:00Z \[Warning\]/);
  assert.match(formatted, /Retrying/);
  assert.match(formatted, /attempt=2/);
  assert.equal(formatDockerLogLine("plain output"), "plain output");
});

test("Docker resource helpers keep selection and filtering stable", () => {
  const container = { id: "abc", name: "api", state: "running", image: "example/api:latest" };
  assert.equal(resourceKey("containers", container), "abc");
  assert.equal(resourceTone("containers", container), "good");
  assert.ok(resourceSearchValues("containers", container).includes("example/api:latest"));
  assert.equal(summarizePorts({ "8080/tcp": [{ HostIp: "127.0.0.1", HostPort: "8080" }] }), "127.0.0.1:8080->8080/tcp");
});
