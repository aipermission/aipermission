#!/usr/bin/env node

const fs = require("node:fs");
const path = require("node:path");
const { execFileSync } = require("node:child_process");

const root = path.resolve(__dirname, "..");

function budgetSnapshot(checkSource, architectureSource = "", functionBudgetSource = "", backendArchitectureSource = "") {
  const architecture = architectureSource ? JSON.parse(architectureSource) : null;
  const frontendLines = architecture?.maxProductionModuleLines ?? sourceBudget(checkSource, "frontend/src");
  const snapshot = {
    "frontend.maxDependencyFanout": architecture?.maxDependencyFanout ?? Number.POSITIVE_INFINITY,
    "frontend.maxProductionModuleLines": frontendLines,
  };
  for (const name of ["connectorSourceBudget", "backendPackageBudget", "suppressionBudget"]) {
    const match = checkSource.match(new RegExp(`const\\s+${name}\\s*=\\s*(\\d+)`));
    if (!match) throw new Error(`Could not read ${name} from maintenance-budget-check.js`);
    snapshot[name] = Number(match[1]);
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
    for (const [entry, value] of budgetMap(checkSource, name)) snapshot[`${prefix}.${entry}`] = value;
  }
  Object.assign(snapshot, goFunctionBudgets(functionBudgetSource), backendFanoutBudgets(backendArchitectureSource));
  return snapshot;
}

function goFunctionBudgets(source) {
  if (!source) return {};
  const lines = goInteger(source, "defaultMaxLines");
  const complexity = goInteger(source, "defaultMaxComplexity");
  const snapshot = {
    "go.function.default.lines": lines,
    "go.function.default.complexity": complexity,
  };
  const match = source.match(/var\s+overrides\s*=\s*map\[string\]budget\s*\{([\s\S]*?)\n\s*\}/);
  if (!match) throw new Error("Could not read Go function overrides");
  for (const entry of match[1].matchAll(/"([^"]+)"\s*:\s*\{\s*lines:\s*([^,]+),\s*complexity:\s*([^}\n]+)\}/g)) {
    snapshot[`go.function.override.${entry[1]}.lines`] = goBudgetValue(entry[2], { defaultMaxLines: lines, defaultMaxComplexity: complexity });
    snapshot[`go.function.override.${entry[1]}.complexity`] = goBudgetValue(entry[3], {
      defaultMaxLines: lines,
      defaultMaxComplexity: complexity,
    });
  }
  return snapshot;
}

function backendFanoutBudgets(source) {
  if (!source) return {};
  const defaultBudget = goInteger(source, "defaultBudget");
  const snapshot = { "go.fanout.default": defaultBudget };
  const match = source.match(/overrides\s*:=\s*map\[string\]int\s*\{([\s\S]*?)\n\s*\}/);
  if (!match) throw new Error("Could not read backend fan-out overrides");
  for (const entry of match[1].matchAll(/modulePath\s*\+\s*"([^"]+)"\s*:\s*(\d+)/g)) {
    snapshot[`go.fanout.override.${entry[1]}`] = Number(entry[2]);
  }
  return snapshot;
}

function goInteger(source, name) {
  const match = source.match(new RegExp(`\\b${name}\\s*=\\s*(\\d+)`));
  if (!match) throw new Error(`Could not read ${name}`);
  return Number(match[1]);
}

function goBudgetValue(value, constants) {
  const normalized = String(value).trim();
  if (/^\d+$/.test(normalized)) return Number(normalized);
  if (Object.hasOwn(constants, normalized)) return constants[normalized];
  throw new Error(`Could not read Go budget value ${normalized}`);
}

function sourceBudget(checkSource, directory) {
  const escaped = directory.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const match = checkSource.match(new RegExp(`directory:\\s*"${escaped}"[\\s\\S]*?maxLines:\\s*(\\d+)`));
  if (!match) throw new Error(`Could not read source budget for ${directory}`);
  return Number(match[1]);
}

function budgetMap(checkSource, name) {
  const match = checkSource.match(new RegExp(`const\\s+${name}\\s*=\\s*new Map\\((?:\\[([\\s\\S]*?)\\])?\\);`));
  if (!match) throw new Error(`Could not read ${name}`);
  return [...String(match[1] || "").matchAll(/\["([^"]+)",\s*(\d+)\]/g)].map((entry) => [entry[1], Number(entry[2])]);
}

function budgetIncreases(base, current) {
  return Object.entries(current).flatMap(([name, value]) => {
    if (!Object.hasOwn(base, name)) return [`${name} is a new unreviewed budget (${value})`];
    return value > base[name] ? [`${name} increased from ${base[name]} to ${value}`] : [];
  });
}

function git(...args) {
  return execFileSync("git", args, { cwd: root, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] }).trim();
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
    console.log("Maintenance budget ratchet skipped: no distinct base revision is available.");
    return;
  }
  const current = budgetSnapshot(
    fs.readFileSync(path.join(root, "scripts/maintenance-budget-check.js"), "utf8"),
    fs.readFileSync(path.join(root, "frontend/architecture-policy.json"), "utf8"),
    fs.readFileSync(path.join(root, "backend/cmd/function-budget/main.go"), "utf8"),
    fs.readFileSync(path.join(root, "backend/internal/architecture/import_boundaries_test.go"), "utf8"),
  );
  const base = budgetSnapshot(
    sourceAt(baseRef, "scripts/maintenance-budget-check.js"),
    optionalSourceAt(baseRef, "frontend/architecture-policy.json"),
    sourceAt(baseRef, "backend/cmd/function-budget/main.go"),
    sourceAt(baseRef, "backend/internal/architecture/import_boundaries_test.go"),
  );
  const failures = budgetIncreases(base, current);
  if (failures.length > 0) {
    console.error("Maintenance budget ratchet failed:");
    failures.forEach((failure) => console.error(`- ${failure}`));
    process.exitCode = 1;
    return;
  }
  console.log(`Maintenance budget ratchet passed against ${baseRef.slice(0, 12)}.`);
}

if (require.main === module) run();

module.exports = { backendFanoutBudgets, budgetIncreases, budgetSnapshot, goFunctionBudgets };
