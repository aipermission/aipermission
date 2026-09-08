const assert = require("node:assert/strict");
const path = require("node:path");
const { execFileSync } = require("node:child_process");
const test = require("node:test");

const root = path.resolve(__dirname, "..");

function composeConfig(file) {
  const migrationPort = "43211";
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
      },
    },
  );
  return { config: JSON.parse(output), migrationPort };
}

for (const file of ["docker-compose.yml", "docker-compose.release.yml"]) {
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
