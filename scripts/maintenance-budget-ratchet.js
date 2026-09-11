#!/usr/bin/env node

const fs = require("node:fs");
const path = require("node:path");
const { execFileSync } = require("node:child_process");

const root = path.resolve(__dirname, "..");

function budgetSnapshot(
  checkSource,
  architectureSource = "",
  functionBudgetSource = "",
  backendArchitectureSource = "",
) {
  const architecture = architectureSource
    ? JSON.parse(architectureSource)
    : null;
  const frontendLines =
    architecture?.maxProductionModuleLines ??
    sourceBudget(checkSource, "frontend/src");
  const snapshot = {
    "frontend.maxDependencyFanout":
      architecture?.maxDependencyFanout ?? Number.POSITIVE_INFINITY,
    "frontend.maxProductionModuleLines": frontendLines,
  };
  for (const name of [
    "connectorSourceBudget",
    "backendPackageBudget",
    "suppressionBudget",
  ]) {
    const match = checkSource.match(
      new RegExp(`const\\s+${name}\\s*=\\s*(\\d+)`),
    );
    if (!match)
      throw new Error(
        `Could not read ${name} from maintenance-budget-check.js`,
      );
    snapshot[name] = Number(match[1]);
  }
  for (const name of [
    "backendTestSourceBudget",
    "frontendTestSourceBudget",
    "mcpTestSourceBudget",
    "backendTestPackageBudget",
    "frontendTestPackageBudget",
    "mcpTestPackageBudget",
  ]) {
    const match = checkSource.match(
      new RegExp(`const\\s+${name}\\s*=\\s*(\\d+)`),
    );
    if (match) snapshot[name] = Number(match[1]);
  }
  for (const [directory, key] of [
    ["backend", "source.backend.maxLines"],
    ["packages/mcp/src", "source.mcp.maxLines"],
  ]) {
    snapshot[key] = sourceBudget(checkSource, directory);
  }
  for (const [name, prefix] of [
    ["sourceBudgetOverrides", "source.override"],
    ["backendPackageBudgetOverrides", "backend.package"],
  ]) {
    for (const [entry, value] of budgetMap(checkSource, name))
      snapshot[`${prefix}.${entry}`] = value;
  }
  Object.assign(
    snapshot,
    goFunctionBudgets(functionBudgetSource),
    backendFanoutBudgets(backendArchitectureSource),
  );
  return snapshot;
}

function goFunctionBudgets(source) {
  if (!source) return {};
  const lines = goInteger(source, "defaultMaxLines");
  const complexity = goInteger(source, "defaultMaxComplexity");
  const testLines = optionalGoInteger(source, "defaultMaxTestLines");
  const testComplexity = optionalGoInteger(source, "defaultMaxTestComplexity");
  const snapshot = {
    "go.function.default.lines": lines,
    "go.function.default.complexity": complexity,
  };
  if (testLines !== undefined) snapshot["go.function.test.lines"] = testLines;
  if (testComplexity !== undefined)
    snapshot["go.function.test.complexity"] = testComplexity;
  const match = source.match(
    /var\s+overrides\s*=\s*map\[string\]budget\s*\{([\s\S]*?)\n\s*\}/,
  );
  if (!match) return snapshot;
  for (const entry of match[1].matchAll(
    /"([^"]+)"\s*:\s*\{\s*lines:\s*([^,]+),\s*complexity:\s*([^}\n]+)\}/g,
  )) {
    snapshot[`go.function.override.${entry[1]}.lines`] = goBudgetValue(
      entry[2],
      { defaultMaxLines: lines, defaultMaxComplexity: complexity },
    );
    snapshot[`go.function.override.${entry[1]}.complexity`] = goBudgetValue(
      entry[3],
      {
        defaultMaxLines: lines,
        defaultMaxComplexity: complexity,
      },
    );
  }
  return snapshot;
}

function backendFanoutBudgets(source) {
  if (!source) return {};
  const legacyBudget = optionalGoInteger(source, "defaultBudget");
  const packageBudget = optionalGoInteger(source, "packageBudget");
  const ownerBudget = optionalGoInteger(source, "ownerBudget");
  if (legacyBudget === undefined && packageBudget === undefined)
    throw new Error("Could not read backend package fan-out budget");
  const snapshot = {
    "go.fanout.package": packageBudget ?? legacyBudget,
  };
  if (ownerBudget !== undefined) snapshot["go.fanout.owner"] = ownerBudget;
  const maxTestImports = optionalGoInteger(
    source,
    "maxTestFileInternalImports",
  );
  if (maxTestImports !== undefined)
    snapshot["go.testImports.maxPerFile"] = maxTestImports;
  const match = source.match(
    /overrides\s*:=\s*map\[string\]int\s*\{([\s\S]*?)\n\s*\}/,
  );
  if (match) {
    for (const entry of match[1].matchAll(
      /modulePath\s*\+\s*"([^"]+)"\s*:\s*(\d+)/g,
    )) {
      snapshot[`go.fanout.override.${entry[1]}`] = Number(entry[2]);
    }
  }
  return snapshot;
}

function goInteger(source, name) {
  const match = source.match(new RegExp(`\\b${name}\\s*=\\s*(\\d+)`));
  if (!match) throw new Error(`Could not read ${name}`);
  return Number(match[1]);
}

function optionalGoInteger(source, name) {
  const match = source.match(new RegExp(`\\b${name}\\s*=\\s*(\\d+)`));
  return match ? Number(match[1]) : undefined;
}

function goBudgetValue(value, constants) {
  const normalized = String(value).trim();
  if (/^\d+$/.test(normalized)) return Number(normalized);
  if (Object.hasOwn(constants, normalized)) return constants[normalized];
  throw new Error(`Could not read Go budget value ${normalized}`);
}

function sourceBudget(checkSource, directory) {
  const escaped = directory.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const match = checkSource.match(
    new RegExp(`directory:\\s*"${escaped}"[\\s\\S]*?maxLines:\\s*(\\d+)`),
  );
  if (!match) throw new Error(`Could not read source budget for ${directory}`);
  return Number(match[1]);
}

function budgetMap(checkSource, name) {
  const match = checkSource.match(
    new RegExp(
      `const\\s+${name}\\s*=\\s*new Map\\((?:\\[([\\s\\S]*?)\\])?\\);`,
    ),
  );
  if (!match) throw new Error(`Could not read ${name}`);
  return [...String(match[1] || "").matchAll(/\["([^"]+)",\s*(\d+)\]/g)].map(
    (entry) => [entry[1], Number(entry[2])],
  );
}

function budgetIncreases(base, current) {
  const removed = Object.keys(base)
    .filter(
      (name) => !Object.hasOwn(current, name) && !isRemovableException(name),
    )
    .map((name) => `${name} was removed from the current maintenance budget`);
  const increases = Object.entries(current).flatMap(([name, value]) => {
    if (!Object.hasOwn(base, name)) {
      const inherited = inheritedBudget(base, name);
      if (inherited !== undefined && value <= inherited) return [];
      return [`${name} is a new unreviewed budget (${value})`];
    }
    if (
      name === "go.fanout.package" &&
      !Object.hasOwn(base, "go.fanout.owner") &&
      current["go.fanout.owner"] <= base[name]
    )
      return [];
    return value > base[name]
      ? [`${name} increased from ${base[name]} to ${value}`]
      : [];
  });
  return [...removed, ...increases];
}

function isRemovableException(name) {
  return [
    "source.override.",
    "backend.package.",
    "go.function.override.",
    "go.fanout.override.",
  ].some((prefix) => name.startsWith(prefix));
}

function inheritedBudget(base, name) {
  const bootstrapCeilings = {
    backendTestSourceBudget: 1800,
    frontendTestSourceBudget: 1000,
    mcpTestSourceBudget: 800,
    backendTestPackageBudget: 15000,
    frontendTestPackageBudget: 3000,
    mcpTestPackageBudget: 1200,
    "go.function.test.lines": 220,
    "go.function.test.complexity": 60,
    "go.testImports.maxPerFile": 14,
  };
  if (Object.hasOwn(bootstrapCeilings, name)) return bootstrapCeilings[name];
  if (name === "go.fanout.owner") return base["go.fanout.package"];
  if (name.startsWith("backend.package.")) return base.backendPackageBudget;
  if (!name.startsWith("source.override.")) return undefined;
  const file = name.slice("source.override.".length);
  if (file.startsWith("frontend/src/"))
    return base["frontend.maxProductionModuleLines"];
  if (file.startsWith("packages/mcp/src/")) return base["source.mcp.maxLines"];
  if (file.startsWith("backend/internal/connectors/")) {
    return Math.min(
      base["source.backend.maxLines"],
      base.connectorSourceBudget,
    );
  }
  if (file.startsWith("backend/")) return base["source.backend.maxLines"];
  return undefined;
}

function git(...args) {
  return execFileSync("git", args, {
    cwd: root,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
  }).trim();
}

function resolveBase() {
  const configured = String(process.env.MAINTENANCE_BUDGET_BASE || "").trim();
  if (configured && !/^0+$/.test(configured)) return configured;
  try {
    return git("merge-base", "HEAD", "origin/main");
  } catch {
    return "";
  }
}

function sourceAt(ref, file) {
  return git("show", `${ref}:${file}`);
}

function optionalSourceAt(ref, file) {
  try {
    return sourceAt(ref, file);
  } catch {
    return "";
  }
}

function run() {
  const baseRef = resolveBase();
  if (!baseRef || baseRef === git("rev-parse", "HEAD")) {
    console.log(
      "Maintenance budget ratchet skipped: no distinct base revision is available.",
    );
    return;
  }
  const current = budgetSnapshot(
    fs.readFileSync(
      path.join(root, "scripts/maintenance-budget-check.js"),
      "utf8",
    ),
    fs.readFileSync(
      path.join(root, "frontend/architecture-policy.json"),
      "utf8",
    ),
    fs.readFileSync(
      path.join(root, "backend/cmd/function-budget/main.go"),
      "utf8",
    ),
    fs.readFileSync(
      path.join(
        root,
        "backend/internal/architecture/import_boundaries_test.go",
      ),
      "utf8",
    ),
  );
  const base = budgetSnapshot(
    sourceAt(baseRef, "scripts/maintenance-budget-check.js"),
    optionalSourceAt(baseRef, "frontend/architecture-policy.json"),
    sourceAt(baseRef, "backend/cmd/function-budget/main.go"),
    sourceAt(
      baseRef,
      "backend/internal/architecture/import_boundaries_test.go",
    ),
  );
  const failures = budgetIncreases(base, current);
  if (failures.length > 0) {
    console.error("Maintenance budget ratchet failed:");
    failures.forEach((failure) => console.error(`- ${failure}`));
    process.exitCode = 1;
    return;
  }
  console.log(
    `Maintenance budget ratchet passed against ${baseRef.slice(0, 12)}.`,
  );
}

if (require.main === module) run();

module.exports = {
  backendFanoutBudgets,
  budgetIncreases,
  budgetSnapshot,
  goFunctionBudgets,
};
