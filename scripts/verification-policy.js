#!/usr/bin/env node

const fs = require("node:fs");
const path = require("node:path");
const { execFileSync } = require("node:child_process");
const { isDeepStrictEqual } = require("node:util");
const { resolveTrustedBase } = require("./trusted-git-base");
const {
  plainObject,
  verifyActionPinsInSource,
  verifyExternalActionPins,
  verifyRequiredWorkflows,
  workflowJobContracts,
  workflowJobs,
} = require("./workflow-contracts");

const root = path.resolve(__dirname, "..");
const policyPath = path.join(__dirname, "verification-policy.json");

function loadPolicy() {
  const policy = JSON.parse(fs.readFileSync(policyPath, "utf8"));
  for (const key of [
    "required_checks",
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
  validateRequiredCheckMigrations(policy);
  validateRequiredCommandMigrations(policy);
  return policy;
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
      if (
        !commands.has(command) &&
        !allowsRequiredCommandMigration(
          previousGate.name,
          command,
          previous,
          policy,
        )
      ) {
        throw new Error(
          `required_checks removed ${previousGate.name} command ${command}`,
        );
      }
      const previousDirectories =
        previousGate.command_working_directories || {};
      const currentDirectory =
        currentGate.command_working_directories?.[command] || ".";
      if (
        Object.hasOwn(previousDirectories, command) &&
        currentDirectory !== previousDirectories[command]
      ) {
        throw new Error(
          `required_checks changed ${previousGate.name} command context for ${command}`,
        );
      }
    }
  }
  for (const key of [
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
    if (process.argv[2] === "--verify-workflows") verifyWorkflows();
    else if (process.argv[2] === "--verify-ratchet") verifyRatchet();
    else if (process.argv[2] === "--list" && process.argv[3]) {
      list(process.argv[3]);
    } else {
      throw new Error(
        "usage: verification-policy.js --verify-workflows | --list POLICY_KEY",
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
  verifyRatchet,
  verifyWorkflows,
  verifyActionPinsInSource,
  verifyExternalActionPins,
  workflowJobContracts,
  workflowJobs,
  validateRequiredCheckMigrations,
  validateRequiredCommandMigrations,
};
