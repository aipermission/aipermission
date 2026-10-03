const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { execFileSync } = require("node:child_process");
const test = require("node:test");
const { assertComposeLoopback } = require("../compose-loopback-policy");

const root = path.resolve(__dirname, "../..");

test("Compose environment example excludes MCP process-only variables", () => {
  const source = fs.readFileSync(path.join(root, ".env.example"), "utf8");
  assert.doesNotMatch(source, /^AIPERMISSION_API_URL=/m);
  assert.match(source, /^AIPERMISSION_MCP_API_URL=/m);
});

function composeConfig(file) {
  const migrationPort = "43211";
  const frontendPort = "43210";
  const output = execFileSync(
    "docker",
    [
      "compose",
      "-f",
      file,
      "--profile",
      "migrate",
      "config",
      "--format",
      "json",
    ],
    {
      cwd: root,
      encoding: "utf8",
      env: {
        ...process.env,
        AIPERMISSION_MIGRATION_FRONTEND_PORT: migrationPort,
        AIPERMISSION_FRONTEND_PORT: frontendPort,
      },
    },
  );
  return { config: JSON.parse(output), migrationPort, frontendPort };
}

for (const file of ["docker-compose.yml", "docker-compose.release.yml"]) {
  test(`${file} exposes the Docker host alias to connector transports`, () => {
    const { config, frontendPort } = composeConfig(file);
    assertComposeLoopback(config, frontendPort);
    assert.equal(config.services.backend.network_mode, "service:frontend");
    assert.equal(config.services.backend.extra_hosts, undefined);
    assert.deepEqual(config.services.frontend.extra_hosts, [
      "host.docker.internal=host-gateway",
    ]);
  });

  test(`${file} loopback guard rejects published API and mutated ingress`, () => {
    const { config, frontendPort } = composeConfig(file);
    const port = config.services.frontend.ports[0];
    for (const [service, field, value] of [
      ["frontend", "ports", undefined],
      ["frontend", "ports", [port, port]],
      ...["0.0.0.0", "::", undefined].map((host_ip) => [
        "frontend",
        "ports",
        [{ ...port, host_ip }],
      ]),
      ["frontend", "ports", [{ ...port, target: 8080 }]],
      ["backend", "ports", [port]],
      ["backend", "network_mode", "host"],
      [
        "backend",
        "environment",
        {
          ...config.services.backend.environment,
          AIPERMISSION_BACKEND_HOST: "0.0.0.0",
        },
      ],
    ]) {
      const changed = structuredClone(config);
      changed.services[service][field] = value;
      assert.throws(
        () => assertComposeLoopback(changed, frontendPort),
        /Compose loopback topology/,
      );
    }
  });

  test(`${file} keeps migration behind its loopback proxy`, () => {
    const { config, migrationPort } = composeConfig(file);
    const migration = config.services.migration;
    const proxy = config.services["migration-proxy"];

    assert.equal(migration.network_mode, "service:migration-proxy");
    assert.deepEqual(migration.depends_on, {
      "migration-proxy": {
        condition: "service_started",
        required: true,
      },
    });
    assert.equal(
      migration.environment.AIPERMISSION_MIGRATION_HOST,
      "127.0.0.1",
    );
    assert.equal(
      String(migration.environment.AIPERMISSION_MIGRATION_PORT),
      "3212",
    );
    assert.equal(migration.ports, undefined);
    assert.deepEqual(proxy.ports, [
      {
        mode: "ingress",
        target: 3211,
        published: migrationPort,
        protocol: "tcp",
        host_ip: "127.0.0.1",
      },
    ]);
  });
}
