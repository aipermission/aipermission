import { spawnSync } from "node:child_process";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const repositoryRoot = resolve(frontendRoot, "..");
const base = duplicationBase();
const cli = join(frontendRoot, "node_modules", "jscpd", "run-jscpd.js");
const result = spawnSync(
  process.execPath,
  [cli, "--config", ".jscpd.json", "--baseline-from-ref", base, "--fail-on-new-clones", "0", "--no-tips"],
  { cwd: frontendRoot, stdio: "inherit" },
);
if (result.error) throw result.error;
process.exit(result.status ?? 1);

function duplicationBase() {
  const requested = process.env.FRONTEND_DUPLICATION_BASE;
  if (requested && !/^0+$/.test(requested)) {
    if (gitRefExists(requested)) return requested;
    throw new Error(`Configured frontend duplication base does not exist: ${requested}`);
  }
  for (const candidate of ["origin/main", "HEAD^"]) {
    if (gitRefExists(candidate)) return candidate;
  }
  throw new Error("Cannot determine a base commit for frontend duplication detection");
}

function gitRefExists(ref) {
  return spawnSync("git", ["rev-parse", "--verify", ref], { cwd: repositoryRoot, stdio: "ignore" }).status === 0;
}
