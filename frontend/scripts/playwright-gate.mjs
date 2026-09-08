import { spawnSync } from "node:child_process";
import { readFileSync, readdirSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import * as currentManifestModule from "./playwright-gate-manifest.mjs";
import {
  requiredAccessibilityTitles,
  requiredHighRiskTitles,
  requiredRealBackendTitles,
  requiredSmokeTitles,
} from "./playwright-gate-manifest.mjs";
import { assertPlaywrightListing, assertPlaywrightManifestRatchet, forbiddenPlaywrightAnnotations } from "./playwright-gate-policy.mjs";

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
assertPlaywrightListing(listTests(["--grep", "@accessibility"]), requiredAccessibilityTitles, "accessibility Playwright");
assertPlaywrightListing(listTests(["--grep-invert", "@high-risk|@accessibility"]), requiredSmokeTitles, "smoke Playwright");
assertPlaywrightListing(listTests(["--config", "playwright.real.config.js"]), requiredRealBackendTitles, "real-backend Playwright");
await assertBaseManifestRatchet();
console.log(
  `Playwright policy passed for ${requiredHighRiskTitles.length} high-risk, ${requiredAccessibilityTitles.length} accessibility, ${requiredSmokeTitles.length} smoke, and ${requiredRealBackendTitles.length} real tests.`,
);

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

async function assertBaseManifestRatchet() {
  const baseRef = resolveBaseRef();
  if (!baseRef || baseRef === git(["rev-parse", "HEAD"])) return;
  const source = git(["show", `${baseRef}:frontend/scripts/playwright-gate-manifest.mjs`], true);
  if (!source) return;
  const encoded = Buffer.from(source).toString("base64");
  const baseModule = await import(`data:text/javascript;base64,${encoded}#${baseRef}`);
  assertPlaywrightManifestRatchet(manifestSnapshot(baseModule), manifestSnapshot(currentManifestModule));
}

function manifestSnapshot(module) {
  return {
    highRisk: [...(module.requiredHighRiskTitles || [])],
    accessibility: [...(module.requiredAccessibilityTitles || [])],
    smoke: [...(module.requiredSmokeTitles || [])],
    realBackend: [...(module.requiredRealBackendTitles || [])],
  };
}

function resolveBaseRef() {
  const configured = String(process.env.PLAYWRIGHT_GATE_BASE || "").trim();
  if (configured && !/^0+$/.test(configured)) return configured;
  return git(["merge-base", "HEAD", "origin/main"], true);
}

function git(args, optional = false) {
  const result = spawnSync("git", args, { cwd: resolve(frontendRoot, ".."), encoding: "utf8" });
  if (result.status === 0) return result.stdout.trim();
  if (optional) return "";
  throw new Error(result.stderr.trim() || `git ${args.join(" ")} failed`);
}
