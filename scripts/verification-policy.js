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
    if (!gate || !sameGateLocation(gate, migration.to)) {
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
    const target = gates.get(migration.to.check);
    if (!target || !target.commands?.includes(migration.to.command)) {
      throw new Error(
        `required command migration ${migration.name} does not match its current target`,
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
  const workflowShell = inheritedRunShell(workflow, sourcePath, "workflow");
  const workflowWorkingDirectory = inheritedRunWorkingDirectory(
    workflow,
    sourcePath,
    "workflow",
  );
  const workflowHasEnvironment = Object.hasOwn(workflow, "env");
  const jobs = new Map();
  for (const [jobID, job] of Object.entries(workflow.jobs)) {
    if (!plainObject(job))
      throw new Error(`${sourcePath} job ${jobID} must be a mapping`);
    if (job.steps != null && !Array.isArray(job.steps)) {
      throw new Error(`${sourcePath} job ${jobID} steps must be a sequence`);
    }
    const jobShell = inheritedRunShell(job, sourcePath, `job ${jobID}`);
    const jobWorkingDirectory = inheritedRunWorkingDirectory(
      job,
      sourcePath,
      `job ${jobID}`,
    );
    const steps = (job.steps || []).map((step, index) => {
      if (!plainObject(step)) {
        throw new Error(
          `${sourcePath} job ${jobID} step ${index + 1} must be a mapping`,
        );
      }
      return {
        run: typeof step.run === "string" ? step.run : "",
        uses: typeof step.uses === "string" ? step.uses : "",
        shell: effectiveRunShell(step.shell, jobShell, workflowShell),
        workingDirectory: effectiveWorkingDirectory(
          step["working-directory"],
          jobWorkingDirectory,
          workflowWorkingDirectory,
        ),
        hasEnvironment: Object.hasOwn(step, "env"),
        writesPersistentEnvironment:
          typeof step.run === "string" &&
          /GITHUB_(?:ENV|PATH)/.test(step.run),
        hasCondition: Object.hasOwn(step, "if"),
        hasContinueOnError: Object.hasOwn(step, "continue-on-error"),
      };
    });
    jobs.set(jobID, {
      name: typeof job.name === "string" ? job.name : "",
      source: job,
      steps,
      hasCondition: Object.hasOwn(job, "if"),
      hasContinueOnError: Object.hasOwn(job, "continue-on-error"),
      hasEnvironment:
        workflowHasEnvironment || Object.hasOwn(job, "env"),
    });
  }
  return jobs;
}

function inheritedRunWorkingDirectory(mapping, sourcePath, owner) {
  if (!Object.hasOwn(mapping, "defaults")) return "";
  if (!plainObject(mapping.defaults)) {
    throw new Error(`${sourcePath} ${owner} defaults must be a mapping`);
  }
  if (!Object.hasOwn(mapping.defaults, "run")) return "";
  if (!plainObject(mapping.defaults.run)) {
    throw new Error(`${sourcePath} ${owner} defaults.run must be a mapping`);
  }
  const directory = mapping.defaults.run["working-directory"];
  if (directory == null) return "";
  if (typeof directory !== "string") {
    throw new Error(
      `${sourcePath} ${owner} defaults.run.working-directory must be a string`,
    );
  }
  return directory;
}

function inheritedRunShell(mapping, sourcePath, owner) {
  if (!Object.hasOwn(mapping, "defaults")) return "";
  if (!plainObject(mapping.defaults)) {
    throw new Error(`${sourcePath} ${owner} defaults must be a mapping`);
  }
  if (!Object.hasOwn(mapping.defaults, "run")) return "";
  if (!plainObject(mapping.defaults.run)) {
    throw new Error(`${sourcePath} ${owner} defaults.run must be a mapping`);
  }
  const shell = mapping.defaults.run.shell;
  if (shell == null) return "";
  if (typeof shell !== "string") {
    throw new Error(`${sourcePath} ${owner} defaults.run.shell must be a string`);
  }
  return shell;
}

function effectiveRunShell(stepShell, jobShell, workflowShell) {
  if (stepShell != null && typeof stepShell !== "string") return "invalid";
  return stepShell || jobShell || workflowShell || "";
}

function effectiveWorkingDirectory(stepDirectory, jobDirectory, workflowDirectory) {
  if (stepDirectory != null && typeof stepDirectory !== "string")
    return "invalid";
  return stepDirectory || jobDirectory || workflowDirectory || ".";
}

function workflowJobs(source) {
  return new Map(
    [...workflowJobContracts(source)].map(([name, contract]) => [
      name,
      JSON.stringify(contract.source),
    ]),
  );
}

function unconditionalStep(step, workingDirectory) {
  return (
    !step.hasCondition &&
    !step.hasContinueOnError &&
    !step.hasEnvironment &&
    !step.writesPersistentEnvironment &&
    step.workingDirectory === workingDirectory &&
    (!step.shell || step.shell === "bash")
  );
}

function stepProvidesCommand(step, command, workingDirectory) {
  if (!unconditionalStep(step, workingDirectory)) return false;
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
    if (contract.hasCondition || contract.hasContinueOnError) {
      throw new Error(
        `${gate.workflow} required job ${gate.job} must be unconditional and fail closed`,
      );
    }
    if (
      contract.hasEnvironment ||
      contract.steps.some(
        (step) => step.hasEnvironment || step.writesPersistentEnvironment,
      )
    ) {
      throw new Error(
        `${gate.workflow} required job ${gate.job} must not override the verification environment`,
      );
    }
    if (contract.steps.some((step) => /\bgo\s+test\b[^\n]*-exec(?:=|\s+)true\b/.test(step.run))) {
      throw new Error(
        `${gate.workflow} required job ${gate.job} uses compile-only go test -exec=true instead of runtime evidence`,
      );
    }
    const directories = gate.command_working_directories || {};
    if (!plainObject(directories)) {
      throw new Error(
        `${gate.workflow} required job ${gate.job} has invalid command working directories`,
      );
    }
    for (const command of Object.keys(directories)) {
      if (!gate.commands.includes(command)) {
        throw new Error(
          `${gate.workflow} required job ${gate.job} configures an unknown command working directory`,
        );
      }
    }
    for (const command of gate.commands) {
      const workingDirectory = directories[command] || ".";
      if (
        !contract.steps.some((step) =>
          stepProvidesCommand(step, command, workingDirectory),
        )
      ) {
        throw new Error(
          `${gate.workflow} job ${gate.job} is missing required command ${command} in ${workingDirectory}`,
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

function allowsRequiredCommandMigration(check, command, policy) {
  return (policy.required_command_migrations || []).some(
    (migration) =>
      migration.from.check === check &&
      migration.from.command === command,
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
      if (
        !commands.has(command) &&
        !allowsRequiredCommandMigration(previousGate.name, command, policy)
      )
        throw new Error(
          `required_checks removed ${previousGate.name} command ${command}`,
        );
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
  validateRequiredCommandMigrations,
};
