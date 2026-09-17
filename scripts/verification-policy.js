#!/usr/bin/env node

const fs = require("node:fs");
const crypto = require("node:crypto");
const path = require("node:path");
const { execFileSync } = require("node:child_process");
const { isDeepStrictEqual } = require("node:util");
const { resolveTrustedBase } = require("./trusted-git-base");
const {
  plainObject,
  verifyActionPinsInSource,
  verifyExternalActionPins,
  verifyRepositoryWorkflowRuntimeContracts,
  verifyRequiredWorkflows,
  workflowJobContracts,
  workflowJobs,
} = require("./workflow-contracts");

const root = path.resolve(__dirname, "..");
const policyPath = path.join(__dirname, "verification-policy.json");
const makefileContextKey = "$makefile";

function loadPolicy() {
  const policy = JSON.parse(fs.readFileSync(policyPath, "utf8"));
  for (const key of [
    "required_checks",
    "local_release_targets",
    "recovery_tests",
    "connector_conformance_tests",
    "fuzz_targets",
  ]) {
    if (!Array.isArray(policy[key]) || policy[key].length === 0) {
      throw new Error(`verification policy has no ${key}`);
    }
    const keys = policy[key].map(entryKey);
    if (new Set(keys).size !== keys.length) {
      throw new Error(`verification policy has duplicate ${key}`);
    }
  }
  if (!plainObject(policy.local_release_recipe_hashes)) {
    throw new Error(
      "verification policy local_release_recipe_hashes must be an object",
    );
  }
  validateLocalReleaseRecipeMigrations(policy);
  validateRequiredCheckMigrations(policy);
  validateRequiredCommandMigrations(policy);
  return policy;
}

function validateLocalReleaseRecipeMigrations(policy) {
  const migrations = policy.local_release_recipe_migrations || [];
  if (!Array.isArray(migrations)) {
    throw new Error(
      "verification policy local_release_recipe_migrations must be an array",
    );
  }
  const targets = new Set();
  for (const migration of migrations) {
    if (
      !migration?.target ||
      !migration.reason?.trim() ||
      !/^[a-f0-9]{64}$/.test(migration.from || "") ||
      !/^[a-f0-9]{64}$/.test(migration.to || "") ||
      migration.from === migration.to
    ) {
      throw new Error(
        "verification policy has an incomplete local release recipe migration",
      );
    }
    if (targets.has(migration.target)) {
      throw new Error(
        `verification policy has duplicate local release recipe migration ${migration.target}`,
      );
    }
    targets.add(migration.target);
    const current = policy.local_release_recipe_hashes[migration.target];
    if (current !== migration.from && current !== migration.to) {
      throw new Error(
        `local release recipe migration ${migration.target} does not match its current hash`,
      );
    }
  }
}

function validateRequiredCheckMigrations(policy) {
  const migrations = policy.required_check_migrations || [];
  if (!Array.isArray(migrations)) {
    throw new Error(
      "verification policy required_check_migrations must be an array",
    );
  }
  const names = new Set();
  const gates = new Map(
    (policy.required_checks || []).map((gate) => [gate.name, gate]),
  );
  for (const migration of migrations) {
    if (
      !migration?.name ||
      !migration.reason?.trim() ||
      !plainObject(migration.from) ||
      !plainObject(migration.to)
    ) {
      throw new Error(
        "verification policy has an incomplete required check migration",
      );
    }
    if (names.has(migration.name)) {
      throw new Error(
        `verification policy has duplicate required check migration ${migration.name}`,
      );
    }
    names.add(migration.name);
    const gate = gates.get(migration.name);
    if (
      !gate ||
      (!sameGateLocation(gate, migration.from) &&
        !sameGateLocation(gate, migration.to))
    ) {
      throw new Error(
        `required check migration ${migration.name} does not match its current gate`,
      );
    }
  }
}

function validateRequiredCommandMigrations(policy) {
  const migrations = policy.required_command_migrations || [];
  if (!Array.isArray(migrations)) {
    throw new Error(
      "verification policy required_command_migrations must be an array",
    );
  }
  const names = new Set();
  const gates = new Map(
    (policy.required_checks || []).map((gate) => [gate.name, gate]),
  );
  for (const migration of migrations) {
    if (
      !migration?.name ||
      !migration.reason?.trim() ||
      !plainObject(migration.from) ||
      !plainObject(migration.to) ||
      !migration.from.check ||
      !migration.from.command ||
      !migration.to.check ||
      !migration.to.command
    ) {
      throw new Error(
        "verification policy has an incomplete required command migration",
      );
    }
    if (names.has(migration.name)) {
      throw new Error(
        `verification policy has duplicate required command migration ${migration.name}`,
      );
    }
    names.add(migration.name);
    const source = gates.get(migration.from.check);
    const target = gates.get(migration.to.check);
    const sourceIsCurrent = source?.commands?.includes(migration.from.command);
    const targetIsCurrent = target?.commands?.includes(migration.to.command);
    if (!sourceIsCurrent && !targetIsCurrent) {
      throw new Error(
        `required command migration ${migration.name} does not match its current target`,
      );
    }
  }
}

function verifyWorkflows(policy = loadPolicy()) {
  verifyRequiredWorkflows(policy);
}

function verifyRepositoryWorkflows(policy = loadPolicy()) {
  verifyWorkflows(policy);
  verifyRepositoryWorkflowRuntimeContracts();
}

function releaseCheckTargets(makefileSource) {
  const lines = makefileSource.split(/\r?\n/);
  const start = makeTargetStart(lines, "release-check");
  let declaration = lines[start];
  let cursor = start;
  while (/\\\s*$/.test(declaration)) {
    cursor++;
    if (cursor >= lines.length)
      throw new Error("release-check target has an incomplete continuation");
    declaration = declaration.replace(/\\\s*$/, " ") + lines[cursor].trim();
  }
  return declaration
    .slice(declaration.indexOf(":") + 1)
    .trim()
    .split(/\s+/)
    .filter(Boolean);
}

function makeTargetStart(lines, target) {
  const definitions = [];
  for (let index = 0; index < lines.length; index++) {
    const start = index;
    if (lines[index].startsWith("\t") || /^\s*#/.test(lines[index])) continue;
    let declaration = lines[index];
    while (/\\\s*$/.test(declaration)) {
      index++;
      if (index >= lines.length) break;
      declaration = declaration.replace(/\\\s*$/, " ") + lines[index].trim();
    }
    const separator = declaration.indexOf(":");
    if (separator < 0) continue;
    const targetList = declaration.slice(0, separator).trim();
    if (targetList.includes("=")) continue;
    const targets = targetList
      .split(/\s+/)
      .map((candidate) => candidate.replace(/&$/, ""));
    if (targets.includes(target)) definitions.push(start);
  }
  if (definitions.length === 0) {
    throw new Error(`Makefile has no ${target} target`);
  }
  if (definitions.length > 1) {
    throw new Error(`Makefile has duplicate ${target} target definitions`);
  }
  return definitions[0];
}

function makeTargetSource(makefileSource, target) {
  const lines = makefileSource.split(/\r?\n/);
  const start = makeTargetStart(lines, target);
  const definition = [lines[start].trimEnd()];
  let cursor = start;
  while (/\\\s*$/.test(lines[cursor])) {
    cursor++;
    if (cursor >= lines.length) {
      throw new Error(`${target} has an incomplete target continuation`);
    }
    definition.push(lines[cursor].trimEnd());
  }
  for (cursor++; cursor < lines.length; cursor++) {
    const line = lines[cursor];
    if (line.startsWith("\t")) {
      definition.push(line.trimEnd());
      continue;
    }
    if (line.trim() === "") continue;
    break;
  }
  if (!definition.some((line) => line.startsWith("\t"))) {
    throw new Error(
      `release-check prerequisite ${target} has no Makefile recipe`,
    );
  }
  return definition.join("\n");
}

function localReleaseRecipeHashes(makefileSource, targets) {
  const hashes = Object.fromEntries(
    targets.map((target) => [
      target,
      crypto
        .createHash("sha256")
        .update(makeTargetSource(makefileSource, target))
        .digest("hex"),
    ]),
  );
  hashes[makefileContextKey] = crypto
    .createHash("sha256")
    .update(makefileSource)
    .digest("hex");
  return hashes;
}

function validateMakefileContext(makefileSource) {
  for (const line of makefileSource.split(/\r?\n/)) {
    if (line.startsWith("\t")) continue;
    if (/^\s*(?:-?include|sinclude)\b/.test(line)) {
      throw new Error(
        "release Makefile must not include mutable external makefiles",
      );
    }
  }
  if (/\$\(\s*eval\b/.test(makefileSource)) {
    throw new Error("release Makefile must not generate rules with eval");
  }
}

function verifyLocalReleaseTargets(
  policy = loadPolicy(),
  makefileSource = fs.readFileSync(path.join(root, "Makefile"), "utf8"),
) {
  validateMakefileContext(makefileSource);
  const expected = policy.local_release_targets;
  const actual = releaseCheckTargets(makefileSource);
  if (!isDeepStrictEqual(actual, expected)) {
    throw new Error(
      `release-check prerequisites differ from verification policy\nexpected: ${expected.join(" ")}\nactual:   ${actual.join(" ")}`,
    );
  }
  const hashes = localReleaseRecipeHashes(makefileSource, expected);
  if (!isDeepStrictEqual(hashes, policy.local_release_recipe_hashes)) {
    throw new Error(
      "release-check target recipes differ from verification policy",
    );
  }
}

function list(kind, policy = loadPolicy()) {
  const values = policy[kind];
  if (!Array.isArray(values) || values.length === 0) {
    throw new Error(`verification policy has no ${kind}`);
  }
  for (const value of values) {
    process.stdout.write(
      typeof value === "string"
        ? `${value}\n`
        : `${value.package}\t${value.name}\n`,
    );
  }
}

function entryKey(value) {
  if (typeof value === "string") return value;
  return value.name
    ? `${value.package || value.workflow || ""}:${value.name}`
    : JSON.stringify(value);
}

function sameGateLocation(left, right) {
  return (
    left?.workflow === right?.workflow &&
    left?.job === right?.job &&
    left?.job_name === right?.job_name
  );
}

function sameMigration(left, right) {
  return isDeepStrictEqual(left, right);
}

function trustedMigration(migration, previousMigrations) {
  return (previousMigrations || []).some((previous) =>
    sameMigration(previous, migration),
  );
}

function allowsRequiredCheckMigration(
  previousGate,
  currentGate,
  previous,
  policy,
) {
  return (policy.required_check_migrations || []).some(
    (migration) =>
      migration.name === previousGate.name &&
      sameGateLocation(previousGate, migration.from) &&
      sameGateLocation(currentGate, migration.to) &&
      trustedMigration(migration, previous.required_check_migrations),
  );
}

function allowsRequiredCommandMigration(check, command, previous, policy) {
  return (policy.required_command_migrations || []).some(
    (migration) =>
      migration.from.check === check &&
      migration.from.command === command &&
      trustedMigration(migration, previous.required_command_migrations),
  );
}

function allowsLocalReleaseRecipeMigration(target, from, to, previous, policy) {
  return (policy.local_release_recipe_migrations || []).some(
    (migration) =>
      migration.target === target &&
      migration.from === from &&
      migration.to === to &&
      trustedMigration(migration, previous.local_release_recipe_migrations),
  );
}

function verifyNoRemovals(previous, policy) {
  const currentGates = new Map(
    (policy.required_checks || []).map((gate) => [gate.name, gate]),
  );
  for (const previousGate of previous.required_checks || []) {
    const currentGate = currentGates.get(previousGate.name);
    if (!currentGate) {
      throw new Error(
        `required_checks removed required entry ${previousGate.name}`,
      );
    }
    if (
      !sameGateLocation(currentGate, previousGate) &&
      !allowsRequiredCheckMigration(previousGate, currentGate, previous, policy)
    ) {
      throw new Error(
        `required_checks moved ${previousGate.name} away from ${previousGate.workflow}:${previousGate.job}`,
      );
    }
    const commands = new Set(currentGate.commands || []);
    for (const command of previousGate.commands || []) {
      const commandRemoved = !commands.has(command);
      const commandMigrated =
        commandRemoved &&
        allowsRequiredCommandMigration(
          previousGate.name,
          command,
          previous,
          policy,
        );
      if (commandRemoved && !commandMigrated) {
        throw new Error(
          `required_checks removed ${previousGate.name} command ${command}`,
        );
      }
      const previousDirectories =
        previousGate.command_working_directories || {};
      const currentDirectory =
        currentGate.command_working_directories?.[command] || ".";
      if (
        !commandRemoved &&
        Object.hasOwn(previousDirectories, command) &&
        currentDirectory !== previousDirectories[command]
      ) {
        throw new Error(
          `required_checks changed ${previousGate.name} command context for ${command}`,
        );
      }
    }
  }
  for (const [target, hash] of Object.entries(
    previous.local_release_recipe_hashes || {},
  )) {
    const current = policy.local_release_recipe_hashes?.[target];
    if (
      current !== hash &&
      !allowsLocalReleaseRecipeMigration(
        target,
        hash,
        current,
        previous,
        policy,
      )
    ) {
      throw new Error(
        `local_release_recipe_hashes changed required entry ${target}`,
      );
    }
  }
  for (const key of [
    "local_release_targets",
    "recovery_tests",
    "connector_conformance_tests",
    "fuzz_targets",
  ]) {
    const current = new Set((policy[key] || []).map(entryKey));
    for (const item of previous[key] || []) {
      if (!current.has(entryKey(item))) {
        throw new Error(`${key} removed required entry ${entryKey(item)}`);
      }
    }
  }
}

function verifyRatchet(
  base = process.env.VERIFICATION_POLICY_BASE,
  policy = loadPolicy(),
) {
  base = resolveTrustedBase({
    configured: base,
    variable: "VERIFICATION_POLICY_BASE",
    root,
  });
  const policyEntry = execFileSync(
    "git",
    ["ls-tree", "--name-only", base, "scripts/verification-policy.json"],
    { cwd: root, encoding: "utf8", stdio: ["ignore", "pipe", "inherit"] },
  ).trim();
  if (!policyEntry) {
    verifyNoRemovals(
      {
        required_checks: [],
        local_release_targets: [],
        local_release_recipe_hashes: {},
        recovery_tests: [],
        connector_conformance_tests: [],
        fuzz_targets: [],
      },
      policy,
    );
    return;
  }
  const previous = JSON.parse(
    execFileSync("git", ["show", `${base}:scripts/verification-policy.json`], {
      cwd: root,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "inherit"],
    }),
  );
  verifyNoRemovals(previous, policy);
}

if (require.main === module) {
  try {
    if (process.argv[2] === "--verify-workflows") verifyRepositoryWorkflows();
    else if (process.argv[2] === "--verify-local-release")
      verifyLocalReleaseTargets();
    else if (process.argv[2] === "--verify-ratchet") verifyRatchet();
    else if (process.argv[2] === "--list" && process.argv[3]) {
      list(process.argv[3]);
    } else {
      throw new Error(
        "usage: verification-policy.js --verify-workflows | --verify-local-release | --verify-ratchet | --list POLICY_KEY",
      );
    }
  } catch (error) {
    console.error(error.message);
    process.exit(1);
  }
}

module.exports = {
  loadPolicy,
  verifyNoRemovals,
  verifyLocalReleaseTargets,
  verifyRatchet,
  verifyRepositoryWorkflows,
  verifyWorkflows,
  verifyActionPinsInSource,
  verifyExternalActionPins,
  workflowJobContracts,
  workflowJobs,
  validateRequiredCheckMigrations,
  validateRequiredCommandMigrations,
  validateLocalReleaseRecipeMigrations,
  localReleaseRecipeHashes,
};
