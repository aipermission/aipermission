const { spawnSync } = require("node:child_process");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const policy = require("../../maintenance-policy.json");

const compileArguments = ["test", "-count=1", "-run", "^$", "./..."];

function createNativeRuntimeSuite({
  cgoEnabled,
  label,
  nodePlatform,
  policyPlatform,
  requiredTestsKey,
  resolveEnvironment = () => ({}),
}) {
  const requiredTests = policy[requiredTestsKey];
  const requiredCoverage = policy.backendCoveragePlatformFiles;

  function assertNativePlatform(platform = process.platform) {
    if (platform !== nodePlatform) {
      throw new Error(
        `${label} runtime evidence requires ${nodePlatform}, received ${platform}`,
      );
    }
  }

  function parseCoverageProfile(content) {
    const lines = content.trim().split(/\r?\n/);
    if (!/^mode: (set|count|atomic)$/.test(lines.shift() || "")) {
      throw new Error(`${label} coverage profile has an invalid header`);
    }
    const coverage = new Map();
    for (const line of lines) {
      const fields = line.trim().split(/\s+/);
      const position = fields[0]?.match(/^(.+\.go):\d+\.\d+,\d+\.\d+$/);
      const statements = Number(fields[1]);
      const count = Number(fields[2]);
      if (
        fields.length !== 3 ||
        !position ||
        !Number.isSafeInteger(statements) ||
        statements < 0 ||
        !Number.isSafeInteger(count) ||
        count < 0
      ) {
        throw new Error(
          `${label} coverage profile has an invalid line: ${line}`,
        );
      }
      const marker = "/backend/";
      const index = position[1].indexOf(marker);
      if (index < 0) continue;
      const sourcePath = position[1].slice(index + marker.length);
      const current = coverage.get(sourcePath) || {
        statements: 0,
        covered: 0,
      };
      current.statements += statements;
      if (count > 0) current.covered += statements;
      coverage.set(sourcePath, current);
    }
    return coverage;
  }

  function verifyPlatformCoverage(coverage, required = requiredCoverage) {
    const failures = [];
    for (const [sourcePath, evidence] of Object.entries(required)) {
      if (evidence.platform !== policyPlatform) continue;
      const measured = coverage.get(sourcePath);
      const percent = measured?.statements
        ? (measured.covered * 100) / measured.statements
        : 0;
      if (!measured?.statements || percent < evidence.minimumCoverage) {
        failures.push(
          `${sourcePath}: ${percent.toFixed(1)}% is below ${evidence.minimumCoverage.toFixed(1)}%`,
        );
      }
    }
    return failures;
  }

  function verifyRequiredTestEvents(lines, required = requiredTests) {
    return verifyNativeTestEvents(lines, required);
  }

  function run() {
    assertNativePlatform();
    const nativeEnvironment = resolveEnvironment();
    const compile = spawnSync("go", compileArguments, {
      encoding: "utf8",
      env: {
        ...process.env,
        ...nativeEnvironment,
        CGO_ENABLED: cgoEnabled,
      },
    });
    if (compile.error) throw compile.error;
    if (compile.status !== 0) {
      process.stdout.write(compile.stdout);
      process.stderr.write(compile.stderr);
      throw new Error(`${label} test graph did not compile and start cleanly`);
    }

    const pattern = `^(${requiredTests.map((entry) => entry.name).join("|")})$`;
    const packages = [
      ...new Set(
        requiredTests.map(
          (entry) => `./${entry.package.split("/backend/")[1]}`,
        ),
      ),
    ];
    const directory = fs.mkdtempSync(
      path.join(os.tmpdir(), `aipermission-${policyPlatform}-`),
    );
    const profilePath = path.join(directory, "coverage.out");
    try {
      const result = spawnSync(
        "go",
        [
          "test",
          "-count=1",
          "-json",
          "-covermode=atomic",
          `-coverprofile=${profilePath}`,
          ...packages,
          "-run",
          pattern,
        ],
        {
          encoding: "utf8",
          env: {
            ...process.env,
            ...nativeEnvironment,
            CGO_ENABLED: cgoEnabled,
          },
        },
      );
      if (result.error) throw result.error;
      const testFailures = verifyRequiredTestEvents(
        result.stdout.split(/\r?\n/),
        requiredTests,
      );
      if (result.status !== 0 || testFailures.length > 0) {
        process.stdout.write(result.stdout);
        process.stderr.write(result.stderr);
        throw new Error(
          testFailures.length > 0
            ? `Required ${label} runtime tests did not pass exactly once: ${testFailures.join(", ")}`
            : `${label} runtime behavior tests failed`,
        );
      }
      const coverageFailures = verifyPlatformCoverage(
        parseCoverageProfile(fs.readFileSync(profilePath, "utf8")),
      );
      if (coverageFailures.length > 0) {
        throw new Error(
          `Required ${label} source coverage failed: ${coverageFailures.join(", ")}`,
        );
      }
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  }

  return {
    assertNativePlatform,
    compileArguments,
    parseCoverageProfile,
    requiredCoverage,
    requiredTests,
    run,
    verifyPlatformCoverage,
    verifyRequiredTestEvents,
  };
}

function verifyNativeTestEvents(lines, required) {
  const events = lines.filter(Boolean).map((line) => JSON.parse(line));
  const failures = [];
  for (const expected of required) {
    const terminal = events.filter(
      (event) =>
        event.Package === expected.package &&
        event.Test === expected.name &&
        ["pass", "fail", "skip"].includes(event.Action),
    );
    if (terminal.length !== 1 || terminal[0].Action !== "pass") {
      failures.push(`${expected.package}:${expected.name}`);
    }
  }
  return failures;
}

module.exports = { createNativeRuntimeSuite, verifyNativeTestEvents };
