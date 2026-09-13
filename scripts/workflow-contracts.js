const fs = require("node:fs");
const path = require("node:path");
const { parseDocument } = require("yaml");

const root = path.resolve(__dirname, "..");

function plainObject(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
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
  if (!plainObject(value)) {
    throw new Error(`${sourcePath} must contain a YAML mapping`);
  }
  return value;
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
    throw new Error(
      `${sourcePath} ${owner} defaults.run.shell must be a string`,
    );
  }
  return shell;
}

function effectiveRunShell(stepShell, jobShell, workflowShell) {
  if (stepShell != null && typeof stepShell !== "string") return "invalid";
  return stepShell || jobShell || workflowShell || "";
}

function effectiveWorkingDirectory(
  stepDirectory,
  jobDirectory,
  workflowDirectory,
) {
  if (stepDirectory != null && typeof stepDirectory !== "string") {
    return "invalid";
  }
  return stepDirectory || jobDirectory || workflowDirectory || ".";
}

function workflowJobContracts(source, sourcePath = "workflow") {
  const workflow = parseWorkflow(source, sourcePath);
  if (!plainObject(workflow.jobs)) {
    throw new Error(`${sourcePath} must define jobs`);
  }
  const workflowShell = inheritedRunShell(workflow, sourcePath, "workflow");
  const workflowWorkingDirectory = inheritedRunWorkingDirectory(
    workflow,
    sourcePath,
    "workflow",
  );
  const workflowHasEnvironment = Object.hasOwn(workflow, "env");
  const jobs = new Map();
  for (const [jobID, job] of Object.entries(workflow.jobs)) {
    if (!plainObject(job)) {
      throw new Error(`${sourcePath} job ${jobID} must be a mapping`);
    }
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
          typeof step.run === "string" && /GITHUB_(?:ENV|PATH)/.test(step.run),
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
      hasEnvironment: workflowHasEnvironment || Object.hasOwn(job, "env"),
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

function visitMappings(value, visit) {
  if (Array.isArray(value)) {
    value.forEach((item) => visitMappings(item, visit));
    return;
  }
  if (!plainObject(value)) return;
  visit(value);
  Object.values(value).forEach((item) => visitMappings(item, visit));
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

function verifyRequiredWorkflows(policy) {
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
    if (!contract) {
      throw new Error(`${gate.workflow} is missing required job ${gate.job}`);
    }
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
    if (
      contract.steps.some((step) =>
        /\bgo\s+test\b[^\n]*-exec(?:=|\s+)true\b/.test(step.run),
      )
    ) {
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

module.exports = {
  plainObject,
  verifyActionPinsInSource,
  verifyExternalActionPins,
  verifyRequiredWorkflows,
  workflowJobContracts,
  workflowJobs,
};
