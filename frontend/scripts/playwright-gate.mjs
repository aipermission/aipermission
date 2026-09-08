import { spawnSync } from "node:child_process";
import { readFileSync, readdirSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { requiredHighRiskTitles, requiredRealBackendTitles } from "./playwright-gate-manifest.mjs";
import { assertPlaywrightListing, forbiddenPlaywrightAnnotations } from "./playwright-gate-policy.mjs";

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
for (const directory of ["e2e", "e2e-real"]) {
  for (const file of specFiles(join(frontendRoot, directory))) {
    const violations = forbiddenPlaywrightAnnotations(readFileSync(file, "utf8"));
    if (violations.length > 0) {
      throw new Error(
        `${file} contains forbidden Playwright annotations: ${violations.map(({ kind, line }) => `${kind}:${line}`).join(", ")}`,
      );
    }
  }
}

assertPlaywrightListing(listTests(["--grep", "@high-risk"]), requiredHighRiskTitles, "high-risk Playwright");
assertPlaywrightListing(listTests(["--config", "playwright.real.config.js"]), requiredRealBackendTitles, "real-backend Playwright");
console.log(`Playwright policy passed for ${requiredHighRiskTitles.length} high-risk and ${requiredRealBackendTitles.length} real tests.`);

function listTests(args) {
  const cli = join(frontendRoot, "node_modules", "@playwright", "test", "cli.js");
  const result = spawnSync(process.execPath, [cli, "test", "--list", "--reporter=json", ...args], {
    cwd: frontendRoot,
    encoding: "utf8",
    maxBuffer: 10 * 1024 * 1024,
  });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(result.stderr.trim() || result.stdout.trim() || "Playwright test discovery failed");
  return JSON.parse(result.stdout);
}

function specFiles(directory) {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) return specFiles(path);
    return /\.(?:spec|test)\.[cm]?[jt]sx?$/.test(entry.name) ? [path] : [];
  });
}
