#!/usr/bin/env node

const fs = require("node:fs");
const path = require("node:path");
const { execFileSync } = require("node:child_process");
const { resolveTrustedBase } = require("./trusted-git-base");

const root = path.resolve(__dirname, "..");

function policySnapshot(input) {
  const policy = typeof input === "string" ? JSON.parse(input) : input;
  const snapshot = {
    "frontend.maxDependencyFanout":
      policy.frontendArchitecture.maxDependencyFanout,
    "frontend.maxProductionModuleLines":
      policy.frontendArchitecture.maxProductionModuleLines,
    connectorSourceBudget: policy.connectorSourceMaxLines,
    backendPackageBudget: policy.backendPackage.defaultMaxLines,
    suppressionBudget: policy.frontendSuppressions.maxCount,
    "go.function.default.lines": policy.goFunction.productionMaxLines,
    "go.function.default.complexity": policy.goFunction.productionMaxComplexity,
    "go.function.test.lines": policy.goFunction.testMaxLines,
    "go.function.test.complexity": policy.goFunction.testMaxComplexity,
    "go.fanout.package": policy.backendFanout.packageMax,
    "go.fanout.owner": policy.backendFanout.ownerMax,
    "go.fanout.ownerFamily": policy.backendFanout.familyOwnerMax,
    "go.fanout.test.package": policy.backendFanout.testPackageMax,
    "go.fanout.test.owner": policy.backendFanout.testOwnerMax,
    "go.fanout.test.ownerFamily": policy.backendFanout.testFamilyOwnerMax,
    "go.testImports.maxPerFile":
      policy.backendFanout.testFileInternalImportsMax,
  };
  for (const extension of policy.frontendArchitecture.sourceExtensions) {
    snapshot[`coverage.frontend.extension.${extension}`] = 0;
  }
  for (const marker of policy.frontendArchitecture.testModuleMarkers) {
    snapshot[`coverage.test.marker.${marker}`] = 0;
  }
  for (const file of policy.frontendSuppressions.forbiddenPaths) {
    snapshot[`coverage.suppression.forbidden.${file}`] = 0;
  }
  for (const budget of policy.sourceBudgets) {
    snapshot[`coverage.source.${budget.id}.directory.${budget.directory}`] = 0;
    snapshot[`coverage.source.${budget.id}.classifier.${budget.classifier}`] =
      0;
    for (const extension of budget.extensions) {
      snapshot[`coverage.source.${budget.id}.extension.${extension}`] = 0;
    }
    if (budget.testPackageDepth !== undefined) {
      snapshot[`test.package.depth.${budget.id}`] = budget.testPackageDepth;
    }
    setSourceBudget(snapshot, budget);
  }
  for (const [file, value] of Object.entries(policy.sourceOverrides)) {
    snapshot[`source.override.${file}`] = value;
  }
  for (const [packagePath, floor] of Object.entries(
    policy.backendCoverageFloors || {},
  )) {
    snapshot[`coverage.backend.floor.${packagePath}`] = -floor;
  }
  for (const [directory, value] of Object.entries(
    policy.backendPackage.overrides,
  )) {
    snapshot[`backend.package.${directory}`] = value;
  }
  for (const [name, value] of Object.entries(policy.goFunction.overrides)) {
    snapshot[`go.function.override.${name}.lines`] = value.lines;
    snapshot[`go.function.override.${name}.complexity`] = value.complexity;
  }
  for (const [name, value] of Object.entries(policy.backendFanout.overrides)) {
    snapshot[`go.fanout.override.${name}`] = value;
  }
  return snapshot;
}

function setSourceBudget(snapshot, budget) {
  const key = budget.id.replace(/-([a-z])/g, (_, letter) =>
    letter.toUpperCase(),
  );
  const legacyNames = {
    backend: [
      "source.backend.maxLines",
      "backendTestSourceBudget",
      "backendTestPackageBudget",
    ],
    frontend: [
      "frontend.maxProductionModuleLines",
      "frontendTestSourceBudget",
      "frontendTestPackageBudget",
    ],
    "mcp-source": [
      "source.mcp.maxLines",
      "mcpTestSourceBudget",
      "mcpTestPackageBudget",
    ],
    "mcp-test": [null, "mcpTestSourceBudget", "mcpTestPackageBudget"],
  };
  const names = legacyNames[budget.id] || [
    `source.${budget.id}.maxLines`,
    `${key}TestSourceBudget`,
    `${key}TestPackageBudget`,
  ];
  setConsistent(snapshot, names[0], budget.productionMaxLines);
  setConsistent(snapshot, names[1], budget.testMaxLines);
  setConsistent(snapshot, names[2], budget.testPackageMaxLines);
}

function setConsistent(snapshot, name, value) {
  if (!name || value === undefined) return;
  if (Object.hasOwn(snapshot, name) && snapshot[name] !== value) {
    throw new Error(`conflicting maintenance policy value for ${name}`);
  }
  snapshot[name] = value;
}

function budgetIncreases(base, current) {
  const removed = Object.keys(base)
    .filter(
      (name) =>
        !Object.hasOwn(current, name) &&
        !removedExceptionRemainsProtected(base, current, name),
    )
    .map((name) => `${name} was removed from the current maintenance budget`);
  const increases = Object.entries(current).flatMap(([name, value]) => {
    if (!Object.hasOwn(base, name)) {
      if (name === "coverage.backend.default" && value < 0) return [];
      if (
        name.startsWith("backend.coverage.neutral.") &&
        !Object.hasOwn(base, "coverage.backend.default")
      ) {
        return [];
      }
      if (name.startsWith("coverage.backend.floor.")) {
        const inheritedDefault =
          base["coverage.backend.default"] ??
          current["coverage.backend.default"];
        if (inheritedDefault !== undefined && value <= inheritedDefault)
          return [];
      }
      if (name.startsWith("coverage.") && value === 0) {
        return [];
      }
      const inherited = inheritedBudget(base, name);
      if (inherited !== undefined && value <= inherited) return [];
      return [`${name} is a new unreviewed budget (${value})`];
    }
    if (
      name === "go.fanout.package" &&
      !Object.hasOwn(base, "go.fanout.owner") &&
      current["go.fanout.owner"] <= base[name]
    ) {
      return [];
    }
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

function removedExceptionRemainsProtected(base, current, name) {
  if (!isRemovableException(name)) return false;
  const inherited = inheritedBudget(current, name);
  return inherited !== undefined && inherited <= base[name];
}

function inheritedBudget(base, name) {
  const bootstrapCeilings = {
    backendTestSourceBudget: 1800,
    frontendTestSourceBudget: 1000,
    mcpTestSourceBudget: 800,
    backendTestPackageBudget: 15000,
    frontendTestPackageBudget: 3000,
    mcpTestPackageBudget: 1200,
    "source.repository-tooling.maxLines": 550,
    repositoryToolingTestSourceBudget: 1000,
    repositoryToolingTestPackageBudget: 1500,
    "source.frontend-tooling.maxLines": 550,
    frontendToolingTestSourceBudget: 1000,
    frontendToolingTestPackageBudget: 1000,
    frontendE2eTestSourceBudget: 1000,
    frontendE2eTestPackageBudget: 1200,
    frontendE2eRealTestSourceBudget: 1000,
    frontendE2eRealTestPackageBudget: 1000,
    "source.frontend-public.maxLines": 550,
    frontendPublicTestSourceBudget: 1000,
    frontendPublicTestPackageBudget: 1000,
    "source.mcp-tooling.maxLines": 550,
    mcpToolingTestSourceBudget: 800,
    mcpToolingTestPackageBudget: 1200,
    "test.package.depth.frontend": 3,
    "test.package.depth.frontend-e2e": 0,
    "test.package.depth.frontend-e2e-real": 0,
    "test.package.depth.frontend-public": 0,
    "test.package.depth.mcp-source": 1,
    "test.package.depth.mcp-test": 1,
    "test.package.depth.repository-tooling": 0,
    "test.package.depth.frontend-tooling": 0,
    "test.package.depth.mcp-tooling": 0,
    "go.function.test.lines": 220,
    "go.function.test.complexity": 60,
    "go.fanout.ownerFamily": 25,
    "go.fanout.test.package": 51,
    "go.fanout.test.owner": 45,
    "go.fanout.test.ownerFamily": 45,
    "go.testImports.maxPerFile": 14,
  };
  if (Object.hasOwn(bootstrapCeilings, name)) return bootstrapCeilings[name];
  if (name === "go.fanout.owner") return base["go.fanout.package"];
  if (name.startsWith("backend.package.")) return base.backendPackageBudget;
  if (name.startsWith("go.function.override.")) {
    if (name.endsWith(".lines")) return base["go.function.default.lines"];
    if (name.endsWith(".complexity")) {
      return base["go.function.default.complexity"];
    }
  }
  if (name.startsWith("go.fanout.override.")) return base["go.fanout.package"];
  if (!name.startsWith("source.override.")) return undefined;
  const file = name.slice("source.override.".length);
  if (file.startsWith("frontend/src/")) {
    return base["frontend.maxProductionModuleLines"];
  }
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

function legacyBudgetSnapshot(
  checkSource,
  architectureSource,
  functionSource,
  backendSource,
) {
  const architecture = JSON.parse(architectureSource);
  const number = (source, name) => {
    const match = source.match(new RegExp(`\\b${name}\\s*=\\s*(\\d+)`));
    return match ? Number(match[1]) : undefined;
  };
  const sourceBudget = (directory) => {
    const escaped = directory.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    const match = checkSource.match(
      new RegExp(`directory:\\s*"${escaped}"[\\s\\S]*?maxLines:\\s*(\\d+)`),
    );
    return match ? Number(match[1]) : undefined;
  };
  const snapshot = {
    "frontend.maxDependencyFanout": architecture.maxDependencyFanout,
    "frontend.maxProductionModuleLines": architecture.maxProductionModuleLines,
    connectorSourceBudget: number(checkSource, "connectorSourceBudget"),
    backendPackageBudget: number(checkSource, "backendPackageBudget"),
    suppressionBudget: number(checkSource, "suppressionBudget"),
    backendTestSourceBudget: number(checkSource, "backendTestSourceBudget"),
    frontendTestSourceBudget: number(checkSource, "frontendTestSourceBudget"),
    mcpTestSourceBudget: number(checkSource, "mcpTestSourceBudget"),
    backendTestPackageBudget: number(checkSource, "backendTestPackageBudget"),
    frontendTestPackageBudget: number(checkSource, "frontendTestPackageBudget"),
    mcpTestPackageBudget: number(checkSource, "mcpTestPackageBudget"),
    "source.backend.maxLines": sourceBudget("backend"),
    "source.mcp.maxLines": sourceBudget("packages/mcp/src"),
    "go.function.default.lines": number(functionSource, "defaultMaxLines"),
    "go.function.default.complexity": number(
      functionSource,
      "defaultMaxComplexity",
    ),
    "go.function.test.lines": number(functionSource, "defaultMaxTestLines"),
    "go.function.test.complexity": number(
      functionSource,
      "defaultMaxTestComplexity",
    ),
    "go.fanout.package":
      number(backendSource, "packageBudget") ??
      number(backendSource, "defaultBudget"),
    "go.fanout.owner": number(backendSource, "ownerBudget"),
    "go.testImports.maxPerFile": number(
      backendSource,
      "maxTestFileInternalImports",
    ),
  };
  for (const extension of architecture.sourceExtensions || []) {
    snapshot[`coverage.frontend.extension.${extension}`] = 0;
  }
  for (const marker of architecture.testModuleMarkers || []) {
    snapshot[`coverage.test.marker.${marker}`] = 0;
  }
  return Object.fromEntries(
    Object.entries(snapshot).filter(([, value]) => value !== undefined),
  );
}

function git(...args) {
  return execFileSync("git", args, {
    cwd: root,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
  }).trim();
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

function snapshotAt(ref) {
  const policy = optionalSourceAt(ref, "maintenance-policy.json");
  if (policy) return policySnapshot(policy);
  return legacyBudgetSnapshot(
    sourceAt(ref, "scripts/maintenance-budget-check.js"),
    sourceAt(ref, "frontend/architecture-policy.json"),
    sourceAt(ref, "backend/cmd/function-budget/main.go"),
    sourceAt(ref, "backend/internal/architecture/import_boundaries_test.go"),
  );
}

function resolveBaseReference(configured, gitCommand = git) {
  return resolveTrustedBase({
    configured,
    variable: "MAINTENANCE_BUDGET_BASE",
    root,
    gitCommand,
  });
}

function run() {
  const configured = String(process.env.MAINTENANCE_BUDGET_BASE || "").trim();
  const baseRef = resolveBaseReference(configured);
  const current = policySnapshot(
    fs.readFileSync(path.join(root, "maintenance-policy.json"), "utf8"),
  );
  const failures = budgetIncreases(snapshotAt(baseRef), current);
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
  budgetIncreases,
  legacyBudgetSnapshot,
  policySnapshot,
  resolveBaseReference,
};
