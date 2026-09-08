import { spawnSync } from "node:child_process";
import { readdirSync } from "node:fs";
import { dirname, join, relative, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");

export function retryCoverageFiles(root = frontendRoot) {
  const files = [join(root, "src/lib/local-action-retry.js")];
  const directory = join(root, "src/lib/local-action-retry");
  files.push(
    ...readdirSync(directory, { withFileTypes: true })
      .filter((entry) => entry.isFile() && entry.name.endsWith(".js") && !entry.name.includes(".test."))
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
        "src/lib/api.test.js",
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
