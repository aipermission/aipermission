import { spawnSync } from "node:child_process";
import { readdirSync } from "node:fs";
import { dirname, join, relative, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const retryCoverageTests = [
  "src/test/http/api.test.ts",
  "src/test/http/api-backup-retry.test.ts",
  "src/lib/local-action-retry/records.test.ts",
  "src/lib/local-action-retry/observations.test.ts",
  "src/lib/local-action-retry/command-observations.test.ts",
  "src/lib/local-action-retry/reconciliations.test.ts",
];

export function retryCoverageFiles(root = frontendRoot) {
  const files = [join(root, "src/lib/local-action-retry.ts")];
  const directory = join(root, "src/lib/local-action-retry");
  files.push(
    ...readdirSync(directory, { withFileTypes: true })
      .filter((entry) => entry.isFile() && /\.(?:js|ts)$/.test(entry.name) && !entry.name.includes(".test."))
      .map((entry) => join(directory, entry.name)),
  );
  return files.map((file) => relative(root, file).split(sep).join("/")).sort();
}

export function runRetryCoverage({ root = frontendRoot, spawn = spawnSync, log = console.log } = {}) {
  for (const file of retryCoverageFiles(root)) {
    const result = spawn(
      process.execPath,
      [
        "--experimental-test-coverage",
        `--test-coverage-include=${file}`,
        "--test-coverage-lines=75",
        "--test-coverage-functions=70",
        "--test-coverage-branches=60",
        "--test",
        ...retryCoverageTests,
      ],
      { cwd: root, encoding: "utf8" },
    );
    if (result.error) throw result.error;
    if (result.status !== 0) {
      process.stdout.write(result.stdout || "");
      process.stderr.write(result.stderr || "");
      return result.status ?? 1;
    }
    log(`Retry coverage passed for ${file}.`);
  }
  return 0;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  process.exit(runRetryCoverage());
}
