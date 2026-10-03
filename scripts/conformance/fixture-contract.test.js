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
    protocols: "Mail",
    "kube-api": "Kubernetes",
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
    assert.match(guide, /no-NIC QEMU guest/);
  }
  assert.doesNotMatch(connectorGuide, /not part of every pull request/);
  assert.equal(compose.networks.default.internal, true);
  assert.deepEqual(compose.services.kafka.tmpfs, [
    "/tmp:size=1g,mode=1777,exec",
  ]);
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
  assert.deepEqual(compose.services.runner.volumes, [
    "compiler-cache:/cache",
    "fixture-material:/fixture-material:ro",
    "kube-material:/kube-material:ro",
  ]);
  assert.equal(
    compose.services.runner.environment.SSL_CERT_FILE,
    "/fixture-material/ca.crt",
  );
  const protocolRoot = "backend/testdata/connector-conformance/protocols";
  assert.match(
    read(`${protocolRoot}/Dockerfile`),
    /^FROM debian:bookworm-slim@sha256:[a-f0-9]{64}$/m,
  );
  assert.match(
    read(`${protocolRoot}/postfix.cf`),
    /^default_transport = error:fixture refuses external delivery$/m,
  );
  assert.match(
    read(`${protocolRoot}/postfix.cf`),
    /^relay_transport = error:fixture refuses external relay$/m,
  );
  assert.doesNotMatch(
    read(`${protocolRoot}/start.sh`),
    /cp .*tls\.key .*fixture-material/,
  );
  assert.deepEqual(compose.services["kube-api"].cap_drop, ["ALL"]);
  assert.deepEqual(compose.services["kube-api"].cap_add, ["CHOWN"]);
  assert.deepEqual(compose.services.protocols.volumes, [
    "fixture-material:/fixture-material",
    "kube-material:/kube-material:ro",
  ]);
  const kube = read(
    "backend/testdata/connector-conformance/kubernetes/start.sh",
  );
  assert.match(kube, /--disable-agent --disable-scheduler/);
  assert.match(kube, /--egress-selector-mode disabled/);
  assert.match(
    kube,
    /chmod 600 "\$config" \/kube-material\/observer-token \/kube-material\/scoped-token/,
  );
  const audit = parseYAML(
    "backend/testdata/connector-conformance/kubernetes/audit.yaml",
  );
  assert.deepEqual(
    audit.rules.map((rule) => rule.level),
    ["Metadata", "None"],
  );
  assert.deepEqual(audit.rules[0].verbs, ["patch"]);
  assert.deepEqual(audit.omitStages, ["RequestReceived"]);
  assert.match(
    read(`${protocolRoot}/kubectl-contained`),
    /--kubeconfig=\/kube-material\/scoped\.yaml --request-timeout=15s/,
  );
});

test("Docker daemon qualification cannot reach the host daemon or network", () => {
  const prefix = "backend/testdata/connector-conformance/docker-vm";
  const build = read("backend/testdata/connector-conformance/Dockerfile");
  assert.match(build, /docker:28\.5\.1-dind@sha256:[a-f0-9]{64} AS guest/);
  assert.match(build, /CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go test -c/);
  assert.match(build, /adduser -S -D -u 1000 -s \/bin\/sh/);
  const scenario = read(
    "backend/internal/connectors/conformance/docker_vm_test.go",
  );
  assert.match(scenario, /"-accel", "tcg,thread=multi,tb-size=64"/);
  assert.match(scenario, /"-nic", "none"/);
  assert.doesNotMatch(scenario, /-enable-kvm|-virtfs|-netdev|-drive|-device/);
  const init = read(`${prefix}/init`);
  assert.match(init, /test "\$\(ls \/sys\/class\/net\)" = lo/);
  assert.match(init, /--pull=never --network none/);
  assert.match(init, /DOCKER_RAMDISK=1 dockerd/);
  assert.match(init, /chmod 755 \/fixture\/docker-config/);
  assert.match(init, /trap cleanup EXIT/);
  assert.match(init, /poweroff -f/);
  assert.match(read(`${prefix}/sshd.conf`), /^ListenAddress 127\.0\.0\.1$/m);
  assert.match(
    read(`${prefix}/docker-contained`),
    /--host unix:\/\/\/run\/docker\.sock/,
  );
});
