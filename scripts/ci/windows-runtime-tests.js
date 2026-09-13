#!/usr/bin/env node

const { spawnSync } = require("node:child_process");

const requiredTests = [
  "TestDatabaseOwnershipIsExclusiveAndReleased",
  "TestDatabaseOwnershipIsExclusiveAcrossProcesses",
  "TestMaintenanceConsoleUnsupportedRuntimeFailsClosed",
];

function verifyRequiredTestEvents(lines, required = requiredTests) {
  const events = lines.filter(Boolean).map((line) => JSON.parse(line));
  const failures = [];
  for (const test of required) {
    const terminal = events.filter(
      (event) =>
        event.Test === test && ["pass", "fail", "skip"].includes(event.Action),
    );
    if (terminal.length !== 1 || terminal[0].Action !== "pass") {
      failures.push(test);
    }
  }
  return failures;
}

function run() {
  const pattern = `^(${requiredTests.join("|")})$`;
  const result = spawnSync(
    "go",
    [
      "test",
      "-count=1",
      "-json",
      "./internal/db",
      "./internal/maintenanceconsole",
      "-run",
      pattern,
    ],
    {
      encoding: "utf8",
      env: { ...process.env, CGO_ENABLED: "0" },
    },
  );
  if (result.error) throw result.error;
  const lines = result.stdout.split(/\r?\n/);
  const failures = verifyRequiredTestEvents(lines);
  if (result.status !== 0 || failures.length > 0) {
    process.stdout.write(result.stdout);
    process.stderr.write(result.stderr);
    throw new Error(
      failures.length > 0
        ? `Required Windows runtime tests did not pass exactly once: ${failures.join(", ")}`
        : "Windows runtime behavior tests failed",
    );
  }
}

if (require.main === module) {
  run();
}

module.exports = { requiredTests, verifyRequiredTestEvents };
