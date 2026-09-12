#!/usr/bin/env node

const fs = require("node:fs");
const path = require("node:path");
const { execFileSync } = require("node:child_process");
const { parseDocument } = require("yaml");
const { resolveTrustedBase } = require("./trusted-git-base");

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
    if (!Array.isArray(policy[key]) || policy[key].length === 0)
      throw new Error(`verification policy has no ${key}`);
    const keys = policy[key].map(entryKey);
    if (new Set(keys).size !== keys.length)
      throw new Error(`verification policy has duplicate ${key}`);
  }
  validateRequiredCheckMigrations(policy);
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
    if (!gate || !sameGateLocation(gate, migration.to)) {
      throw new Error(
        `required check migration ${migration.name} does not match its current gate`,
      );
    }
  }
}

function parseWorkflow(source, sourcePath = "workflow") {
  const document = parseDocument(source, {
    maxAliasCount: 0,
    prettyErrors: true,
    strict: true,
    uniqueKeys: true,
  });
  if (document.errors.length > 0) {
    throw new Error(
      `${sourcePath} is invalid YAML: ${document.errors[0].message}`,
    );
  }
  const value = document.toJS({ maxAliasCount: 0 });
  if (!plainObject(value))
    throw new Error(`${sourcePath} must contain a YAML mapping`);
  return value;
}

function plainObject(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function workflowJobContracts(source, sourcePath = "workflow") {
  const workflow = parseWorkflow(source, sourcePath);
  if (!plainObject(workflow.jobs))
    throw new Error(`${sourcePath} must define jobs`);
  const jobs = new Map();
  for (const [jobID, job] of Object.entries(workflow.jobs)) {
    if (!plainObject(job))
      throw new Error(`${sourcePath} job ${jobID} must be a mapping`);
    if (job.steps != null && !Array.isArray(job.steps)) {
      throw new Error(`${sourcePath} job ${jobID} steps must be a sequence`);
    }
    const steps = (job.steps || []).map((step, index) => {
      if (!plainObject(step)) {
        throw new Error(
          `${sourcePath} job ${jobID} step ${index + 1} must be a mapping`,
        );
      }
      return {
        run: typeof step.run === "string" ? step.run : "",
        uses: typeof step.uses === "string" ? step.uses : "",
        shell: typeof step.shell === "string" ? step.shell : "",
        hasCondition: Object.hasOwn(step, "if"),
        hasContinueOnError: Object.hasOwn(step, "continue-on-error"),
      };
    });
    jobs.set(jobID, {
      name: typeof job.name === "string" ? job.name : "",
      source: job,
      steps,
    });
  }
  return jobs;
}

function workflowJobs(source) {
  return new Map(
    [...workflowJobContracts(source)].map(([name, contract]) => [
      name,
      JSON.stringify(contract.source),
    ]),
  );
}

function unconditionalStep(step) {
  return (
    !step.hasCondition &&
    !step.hasContinueOnError &&
    (!step.shell || step.shell === "bash")
  );
}

function stepProvidesCommand(step, command) {
  if (!unconditionalStep(step)) return false;
  if (command.startsWith("uses:")) {
    const expected = command.slice(5);
    return step.uses === expected || step.uses.startsWith(`${expected}@`);
  }
  return step.run.trim() === command;
}

function verifyActionPinsInSource(source, sourcePath) {
  const workflow = parseWorkflow(source, sourcePath);
  visitMappings(workflow, (mapping) => {
    if (!Object.hasOwn(mapping, "uses")) return;
    const reference = mapping.uses;
    if (typeof reference !== "string" || reference.trim() !== reference) {
      throw new Error(`${sourcePath} has an invalid action reference`);
    }
    if (reference.startsWith("./")) return;
    if (reference.startsWith("docker://")) {
      if (!/@sha256:[0-9a-f]{64}$/i.test(reference)) {
        throw new Error(
          `${sourcePath} Docker action ${reference} must use a sha256 digest`,
        );
      }
      return;
    }
    if (!/^[^@\s]+@[0-9a-f]{40}$/i.test(reference)) {
      throw new Error(
        `${sourcePath} external action ${reference} must use a full 40-character commit SHA`,
      );
    }
  });
}

function visitMappings(value, visit) {
  if (Array.isArray(value)) {
    value.forEach((item) => visitMappings(item, visit));
    return;
  }
  if (!plainObject(value)) return;
  visit(value);
  Object.values(value).forEach((item) => visitMappings(item, visit));
}

function workflowFiles() {
  const roots = [
    path.join(root, ".github", "workflows"),
    path.join(root, ".github", "actions"),
  ];
  const files = [];
  const visit = (directory) => {
    if (!fs.existsSync(directory)) return;
    for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
      const absolute = path.join(directory, entry.name);
      if (entry.isDirectory()) visit(absolute);
      else if (/\.ya?ml$/i.test(entry.name)) files.push(absolute);
    }
  };
  roots.forEach(visit);
  return files.sort();
}

function verifyExternalActionPins() {
  for (const file of workflowFiles()) {
    verifyActionPinsInSource(
      fs.readFileSync(file, "utf8"),
      path.relative(root, file),
    );
  }
}

function verifyWorkflows(policy = loadPolicy()) {
  verifyExternalActionPins();
  const parsed = new Map();
  for (const gate of policy.required_checks) {
    if (
      !gate.workflow ||
      !gate.job ||
      !gate.job_name ||
      !Array.isArray(gate.commands) ||
      gate.commands.length === 0
    ) {
      throw new Error(
        `required check ${gate.name || "unnamed"} has an incomplete workflow contract`,
      );
    }
    if (!parsed.has(gate.workflow)) {
      const source = fs.readFileSync(path.join(root, gate.workflow), "utf8");
      const contracts = workflowJobContracts(source, gate.workflow);
      const names = new Set();
      for (const [job, contract] of contracts) {
        if (!contract.name) continue;
        if (contract.name.includes("${{")) {
          throw new Error(
            `${gate.workflow} job ${job} has a dynamic check name ${contract.name}; required check identities must be static`,
          );
        }
        if (names.has(contract.name)) {
          throw new Error(
            `${gate.workflow} has duplicate job check name ${contract.name}`,
          );
        }
        names.add(contract.name);
      }
      parsed.set(gate.workflow, contracts);
    }
    const contract = parsed.get(gate.workflow).get(gate.job);
    if (!contract)
      throw new Error(`${gate.workflow} is missing required job ${gate.job}`);
    if (contract.name !== gate.job_name) {
      throw new Error(
        `${gate.workflow} job ${gate.job} has check name ${contract.name || "missing"}, expected ${gate.job_name}`,
      );
    }
    for (const command of gate.commands) {
      if (!contract.steps.some((step) => stepProvidesCommand(step, command))) {
        throw new Error(
          `${gate.workflow} job ${gate.job} is missing required command ${command}`,
        );
      }
    }
  }
}

function list(kind, policy = loadPolicy()) {
  const values = policy[kind];
  if (!Array.isArray(values) || values.length === 0)
    throw new Error(`verification policy has no ${kind}`);
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

function allowsRequiredCheckMigration(previousGate, currentGate, policy) {
  return (policy.required_check_migrations || []).some(
    (migration) =>
      migration.name === previousGate.name &&
      sameGateLocation(previousGate, migration.from) &&
      sameGateLocation(currentGate, migration.to),
  );
}

function verifyNoRemovals(previous, policy) {
  const currentGates = new Map(
    (policy.required_checks || []).map((gate) => [gate.name, gate]),
  );
  for (const previousGate of previous.required_checks || []) {
    const currentGate = currentGates.get(previousGate.name);
    if (!currentGate)
      throw new Error(
        `required_checks removed required entry ${previousGate.name}`,
      );
    if (
      !sameGateLocation(currentGate, previousGate) &&
      !allowsRequiredCheckMigration(previousGate, currentGate, policy)
    ) {
      throw new Error(
        `required_checks moved ${previousGate.name} away from ${previousGate.workflow}:${previousGate.job}`,
      );
    }
    const commands = new Set(currentGate.commands || []);
    for (const command of previousGate.commands || []) {
      if (!commands.has(command))
        throw new Error(
          `required_checks removed ${previousGate.name} command ${command}`,
        );
    }
  }
  for (const key of [
    "recovery_tests",
    "connector_conformance_tests",
    "fuzz_targets",
  ]) {
    const current = new Set((policy[key] || []).map(entryKey));
    for (const item of previous[key] || []) {
      if (!current.has(entryKey(item)))
        throw new Error(`${key} removed required entry ${entryKey(item)}`);
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
  let previous;
  if (!policyEntry) {
    previous = {
      required_checks: [],
      recovery_tests: [],
      connector_conformance_tests: [],
      fuzz_targets: [],
    };
    verifyNoRemovals(previous, policy);
    return;
  }
  previous = JSON.parse(
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
    else if (process.argv[2] === "--list" && process.argv[3])
      list(process.argv[3]);
    else
      throw new Error(
        "usage: verification-policy.js --verify-workflows | --list POLICY_KEY",
      );
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
};
