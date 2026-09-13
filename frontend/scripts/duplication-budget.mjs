import { spawnSync } from "node:child_process";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { resolveFrontendBase } from "./coverage-git-state.mjs";

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const repositoryRoot = resolve(frontendRoot, "..");
const base = resolveFrontendBase(repositoryRoot, {
  configured: process.env.FRONTEND_DUPLICATION_BASE,
  variable: "FRONTEND_DUPLICATION_BASE",
});
const cli = join(frontendRoot, "node_modules", "jscpd", "run-jscpd.js");
const result = spawnSync(
  process.execPath,
  [cli, "--config", ".jscpd.json", "--baseline-from-ref", base, "--fail-on-new-clones", "0", "--no-tips"],
  { cwd: frontendRoot, stdio: "inherit" },
);
if (result.error) throw result.error;
process.exit(result.status ?? 1);
