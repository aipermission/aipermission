import assert from "node:assert/strict";
import { test } from "vitest";
import {
  arrayOrString,
  formatDockerLogLine,
  formatDockerLogs,
  imageRef,
  resourceKey,
  resourceLabel,
  resourcePlaceholder,
  resourcePrimary,
  resourceSearchValues,
  resourceSecondary,
  resourceSingular,
  resourceStatus,
  resourceTabLabel,
  resourceTertiary,
  resourceTone,
  shortValue,
  stripSlash,
  summarizeNetworks,
  summarizePorts,
} from "./helpers.ts";
import type { DockerResourceKind } from "./resource-types";

test("Docker log formatting keeps structured messages readable", () => {
  const line = '2026-08-11T00:00:00Z {"Level":"Warning","Message":"Retrying","Properties":{"attempt":2}}';
  const formatted = formatDockerLogLine(line);
  assert.match(formatted, /2026-08-11T00:00:00Z \[Warning\]/);
  assert.match(formatted, /Retrying/);
  assert.match(formatted, /attempt=2/);
  assert.equal(formatDockerLogLine("plain output"), "plain output");
});

test("Docker log projection handles alternate fields and malformed payloads", () => {
  assert.equal(formatDockerLogs("one\ntwo"), "one\ntwo");
  assert.equal(formatDockerLogLine(" "), " ");
  assert.equal(formatDockerLogLine("{broken}"), "{broken}");
  assert.match(
    formatDockerLogLine('{"Timestamp":"now","Severity":"error","RenderedMessage":"Failed","Exception":"oops","Properties":{"code":"X"}}'),
    /now \[error\]\n {2}Failed\n {2}Exception: oops\n {2}code=X/,
  );
  assert.match(formatDockerLogLine('{"level":"info","message":"Ready"}'), /\[info\]\n {2}Ready/);
  assert.match(formatDockerLogLine('{"MessageTemplate":"Template"}'), /Docker log\n {2}Template/);
  assert.equal(stripSlash("/api"), "api");
  assert.equal(arrayOrString(["/bin/sh", "-c"]), "/bin/sh -c");
  assert.equal(arrayOrString("shell"), "shell");
  assert.equal(shortValue(undefined), "");
  assert.equal(shortValue("x".repeat(81)), `${"x".repeat(77)}...`);
  assert.equal(shortValue({ count: 2 }), '{"count":2}');
});

test("Docker metadata projection tolerates absent and malformed bindings", () => {
  assert.equal(summarizePorts(null), "");
  assert.equal(
    summarizePorts({ "80/tcp": [], "81/tcp": null, "82/tcp": [null, {}] }),
    "80/tcp\n81/tcp\n0.0.0.0:->82/tcp\n0.0.0.0:->82/tcp",
  );
  assert.equal(summarizeNetworks(null), "");
  assert.equal(summarizeNetworks({ bridge: { IPAddress: "172.17.0.2" }, isolated: null }), "bridge 172.17.0.2\nisolated");
});

test("Docker resource modes retain labels, identity, search, and placeholders", () => {
  const modes: DockerResourceKind[] = ["containers", "images", "networks", "volumes"];
  assert.deepEqual(modes.map(resourceLabel), ["Containers", "Images", "Networks", "Volumes"]);
  assert.deepEqual(modes.map(resourceTabLabel), ["Ctrs", "Images", "Nets", "Vols"]);
  assert.deepEqual(modes.map(resourceSingular), ["container", "image", "network", "volume"]);
  for (const mode of modes) {
    assert.ok(resourcePlaceholder(mode).length > 10);
    assert.ok(resourceSearchValues(mode, {}).length > 0);
    assert.equal(resourceTone(mode, {}), "neutral");
  }
  const image = { repository: "api", tag: "latest", id: "image-id", size: "20 MB", created_at: "today", containers: 2 };
  assert.equal(imageRef(image), "api:latest");
  assert.equal(imageRef({ repository: "api", tag: "<none>" }), "api");
  assert.equal(imageRef({ id: "image-id" }), "image-id");
  assert.equal(imageRef(), "-");
  assert.equal(resourceKey("images", image), "image-id");
  assert.equal(resourceKey("images", { repository: "api", tag: "latest" }), "api:latest");
  assert.equal(resourcePrimary("images", image), "api:latest");
  assert.equal(resourceSecondary("images", image), "image-id · 20 MB");
  assert.equal(resourceTertiary("images", image), "today · 2 containers");
  assert.equal(resourceStatus("images", image), "20 MB");
  const network = { name: "private", driver: "bridge", scope: "local", containers: 2, labels: "owner=test", mountpoint: "/data" };
  for (const mode of ["networks", "volumes"] as const) {
    assert.equal(resourceKey(mode, network), "private");
    assert.equal(resourcePrimary(mode, network), "private");
    assert.equal(resourceTertiary(mode, network), "2 visible containers");
    assert.equal(resourceTertiary(mode, { labels: "owner=test" }), "owner=test");
    assert.equal(resourceStatus(mode, network), "bridge");
  }
  assert.equal(resourceSecondary("networks", network), "bridge · local");
  assert.equal(resourceSecondary("volumes", network), "bridge · /data");
  assert.equal(resourcePrimary("containers"), "-");
  assert.equal(resourceTone("containers", { health: "unhealthy", state: "running" }), "bad");
  assert.equal(resourceStatus("containers"), "unknown");
  assert.equal(resourceSecondary("containers", { image: "api", compose_project: "test" }), "api · project test");
  assert.equal(resourceTertiary("containers", { status: "Up", compose_service: "web" }), "Up · service web");
});

test("Docker resource helpers keep selection and filtering stable", () => {
  const container = { id: "abc", name: "api", state: "running", image: "example/api:latest" };
  assert.equal(resourceKey("containers", container), "abc");
  assert.equal(resourceTone("containers", container), "good");
  assert.ok(resourceSearchValues("containers", container).includes("example/api:latest"));
  assert.equal(summarizePorts({ "8080/tcp": [{ HostIp: "127.0.0.1", HostPort: "8080" }] }), "127.0.0.1:8080->8080/tcp");
});
