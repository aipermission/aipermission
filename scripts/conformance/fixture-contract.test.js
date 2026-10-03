const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");
const { parseYAMLMapping } = require("../workflow-contracts");

const root = path.resolve(__dirname, "../..");
const read = (file) => fs.readFileSync(path.join(root, file), "utf8");
const prose = (value) => value.replace(/\s+/g, " ");
const parseYAML = (file) => parseYAMLMapping(read(file), file);

test("connector conformance documentation matches the required workflow", () => {
  const workflow = parseYAML(".github/workflows/connector-conformance.yml");
  const compose = parseYAML(
    "backend/testdata/connector-conformance/compose.yml",
  );
  const connectorGuide = read("backend/internal/connectors/README.md");
  const testingGuide = read("docs/development/testing.md");

  const triggers = workflow.on;
  assert.deepEqual(triggers.pull_request?.branches, ["main", "dev"]);
  assert.deepEqual(triggers.push?.branches, ["main", "dev"]);
  assert.ok(Object.hasOwn(triggers, "workflow_dispatch"));
  assert.ok(Array.isArray(triggers.schedule) && triggers.schedule.length > 0);
  const families = {
    clickhouse: "ClickHouse",
    postgres: "Postgres",
    valkey: "Valkey",
    rabbitmq: "RabbitMQ",
    minio: "S3",
    kafka: "Kafka",
  };
  for (const service of Object.keys(families)) {
    assert.ok(
      Object.hasOwn(compose.services, service),
      `compose service is missing: ${service}`,
    );
  }
  for (const guide of [connectorGuide, testingGuide].map(prose)) {
    assert.match(guide, /pull requests and pushes to `main` and `dev`/);
    assert.match(guide, /weekly,? and on demand/);
    for (const service of Object.values(families)) {
      assert.match(guide, new RegExp(`\\b${service}\\b`));
    }
  }
  assert.doesNotMatch(connectorGuide, /not part of every pull request/);
  assert.equal(compose.networks.default.internal, true);
  assert.deepEqual(compose.services.kafka.tmpfs, ["/tmp:size=1g,mode=1777,exec"]);
  for (const [name, service] of Object.entries(compose.services)) {
    for (const property of [
      "ports",
      "privileged",
      "network_mode",
      "extra_hosts",
    ]) {
      assert.equal(
        service[property],
        undefined,
        `${name}.${property} escapes the owned fixture`,
      );
    }
    for (const mount of service.volumes || [])
      assert.doesNotMatch(mount, /docker\.sock|kubeconfig|\.ssh|\/run\//);
    if (service.image) assert.match(service.image, /@sha256:[a-f0-9]{64}$/);
  }
  assert.deepEqual(compose.services.runner.cap_drop, ["ALL"]);
  assert.deepEqual(compose.services.runner.security_opt, [
    "no-new-privileges:true",
  ]);
  assert.deepEqual(compose.services.runner.volumes, ["compiler-cache:/cache"]);
});
