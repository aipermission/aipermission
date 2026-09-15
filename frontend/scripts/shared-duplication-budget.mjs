import { spawnSync } from "node:child_process";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { resolveFrontendBase } from "./coverage-git-state.mjs";

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const repositoryRoot = resolve(frontendRoot, "..");
const base = resolveFrontendBase(repositoryRoot, {
  configured: process.env.SHARED_DUPLICATION_BASE,
  variable: "SHARED_DUPLICATION_BASE",
});
const cli = join(frontendRoot, "node_modules", "jscpd", "run-jscpd.js");

for (const { path, format, lines, tokens } of [
  { path: "backend/internal", format: "go", lines: "20", tokens: "150" },
  { path: "packages/mcp/src", format: "javascript,typescript", lines: "12", tokens: "100" },
]) {
  const result = spawnSync(
    process.execPath,
    [
      cli,
      path,
      "--format",
      format,
      "--min-lines",
      lines,
      "--min-tokens",
      tokens,
      "--mode",
      "strict",
      "--ignore",
      "**/*_test.go,**/*.test.js,**/*.test.ts",
      "--baseline-from-ref",
      base,
      "--fail-on-new-clones",
      "0",
      "--no-tips",
    ],
    { cwd: repositoryRoot, stdio: "inherit" },
  );
  if (result.error) throw result.error;
  if (result.status !== 0) process.exit(result.status ?? 1);
}
