#!/usr/bin/env node

const fs = require("node:fs");

function verifyFuzzEvents(source, target, budget = "") {
  let targetRun = false;
  let targetPass = false;
  let targetSkip = false;
  let targetFail = false;
  let packagePass = false;
  let targetPackage = "";
  let fuzzStarted = false;
  let executions = 0;

  for (const [index, line] of source.split(/\r?\n/).entries()) {
    if (!line.trim()) continue;
    let event;
    try {
      event = JSON.parse(line);
    } catch {
      throw new Error(`invalid go test JSON event at line ${index + 1}`);
    }
    if (event.Test === target) {
      if (event.Action === "run") {
        targetRun = true;
        targetPackage = event.Package || "";
      }
      if (targetPackage && event.Package !== targetPackage) continue;
      if (event.Action === "pass") targetPass = true;
      if (event.Action === "skip") targetSkip = true;
      if (event.Action === "fail") targetFail = true;
      if (event.Action === "output") {
        if (/gathering baseline coverage: \d+\/\d+ completed, now fuzzing with \d+ workers/.test(event.Output || "")) {
          fuzzStarted = true;
        }
        const match = /\bexecs:\s*(\d+)\b/.exec(event.Output || "");
        if (match) executions = Math.max(executions, Number(match[1]));
      }
    } else if (
      targetPass &&
      !event.Test &&
      event.Action === "pass" &&
      event.Package === targetPackage
    ) {
      packagePass = true;
    }
  }

  const failures = [];
  if (!targetRun) failures.push("target did not emit a run event");
  if (targetSkip) failures.push("target emitted a skip event");
  if (targetFail) failures.push("target emitted a fail event");
  if (!fuzzStarted) failures.push("target never entered generated-input fuzzing");
  if (executions < 1) failures.push("target reported no fuzz executions");
  const exactExecutions = /^(\d+)x$/.exec(budget);
  if (exactExecutions && executions < Number(exactExecutions[1])) {
    failures.push(`target reported ${executions} executions for ${budget} budget`);
  }
  if (!targetPass) failures.push("target did not emit a pass event");
  if (!packagePass) failures.push("package did not emit a pass event");
  if (failures.length > 0) {
    throw new Error(`${target} execution evidence rejected: ${failures.join("; ")}`);
  }
  return { executions };
}

function main(argv) {
  const [eventsPath, target, budget] = argv;
  if (!eventsPath || !/^Fuzz\w+$/.test(target || "") || !/^\d+(?:x|ms|s)$/.test(budget || "")) {
    throw new Error("usage: verify-fuzz-events.js EVENTS_JSONL FuzzTarget BUDGET");
  }
  const result = verifyFuzzEvents(fs.readFileSync(eventsPath, "utf8"), target, budget);
  console.log(`${target} executed ${result.executions} fuzz inputs.`);
}

if (require.main === module) {
  try {
    main(process.argv.slice(2));
  } catch (error) {
    console.error(error instanceof Error ? error.message : String(error));
    process.exit(1);
  }
}

module.exports = { verifyFuzzEvents };
