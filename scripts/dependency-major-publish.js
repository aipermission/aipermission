#!/usr/bin/env node

const { execFileSync } = require("node:child_process");
const { mkdtempSync, readFileSync, rmSync, writeFileSync } = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { parseArgs } = require("node:util");
const {
  major,
  renderReport,
  isGeneratedReport,
} = require("./dependency-major-report");

const reportTitle = "[maintenance] Deferred major dependency updates";
const issueLimit = 100;

function validateSnapshot(snapshot) {
  if (
    snapshot?.schema_version !== 1 ||
    !Array.isArray(snapshot.updates) ||
    snapshot.updates.length > 1000
  ) {
    throw new Error("Invalid dependency report snapshot");
  }
  const identities = new Set();
  for (const update of snapshot.updates) {
    const validEcosystem =
      update?.ecosystem === "Go" || update?.ecosystem === "npm";
    const validDirectory =
      update?.ecosystem === "Go"
        ? update.directory === "backend"
        : ["frontend", "packages/mcp", "scripts"].includes(update?.directory);
    const versionPattern = /^v?\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.+-]+)?$/;
    if (
      !validEcosystem ||
      !validDirectory ||
      typeof update.name !== "string" ||
      !/^[0-9A-Za-z@_./-]{1,250}$/.test(update.name) ||
      !versionPattern.test(update.current) ||
      !versionPattern.test(update.latest) ||
      major(update.latest) <= major(update.current)
    ) {
      throw new Error("Invalid dependency update record");
    }
    const identity = `${update.ecosystem}:${update.directory}:${update.name}`;
    if (identities.has(identity))
      throw new Error("Duplicate dependency update record");
    identities.add(identity);
  }
  return snapshot.updates;
}

function managedIssues(issues, adoptIssueNumber) {
  if (!Array.isArray(issues) || issues.length >= issueLimit)
    throw new Error("Dependency issue search is incomplete");
  return issues.filter((issue) => {
    const botAuthor =
      issue.author?.login === "app/github-actions" ||
      issue.author?.login === "github-actions[bot]";
    if (
      issue.title !== reportTitle ||
      !botAuthor ||
      !issue.labels?.some((label) => label.name === "maintenance")
    )
      return false;
    if (
      !Number.isSafeInteger(issue.number) ||
      issue.number < 1 ||
      !["OPEN", "CLOSED"].includes(issue.state) ||
      typeof issue.body !== "string"
    ) {
      throw new Error("Invalid generated issue metadata");
    }
    // Adoption is an explicit one-time workflow decision, not a title-only match.
    const markedReport = isGeneratedReport(issue.body);
    if (
      !markedReport &&
      !(
        issue.number === adoptIssueNumber &&
        issue.body === renderReport([], { includeMarker: false })
      )
    ) {
      if (issue.state === "CLOSED") return false;
      throw new Error(
        "Generated issue ownership is ambiguous; maintainer review required",
      );
    }
    return true;
  });
}

function planPublication(snapshot, issues, { adoptIssueNumber = 0 } = {}) {
  const updates = validateSnapshot(snapshot);
  const managed = managedIssues(issues, adoptIssueNumber);
  const open = managed.filter((issue) => issue.state === "OPEN");
  if (open.length > 1)
    throw new Error("Multiple generated dependency issues are open");
  const current = open[0];
  if (updates.length === 0)
    return current
      ? { action: "close", number: current.number }
      : { action: "none" };
  const body = renderReport(updates);
  if (current)
    return current.body === body
      ? { action: "none" }
      : { action: "update", number: current.number, body };
  const previous = managed
    .filter((issue) => issue.state === "CLOSED")
    .sort((a, b) => b.number - a.number)[0];
  return previous
    ? { action: "reopen", number: previous.number, body }
    : { action: "create", body };
}

function runGitHub(args) {
  return execFileSync("gh", args, {
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
    timeout: 60_000,
  });
}

function publishReport(snapshot, repository, run = runGitHub, options = {}) {
  validateSnapshot(snapshot);
  if (typeof repository !== "string" || !/^[\w.-]+\/[\w.-]+$/.test(repository))
    throw new Error("A GitHub owner/repository is required");
  const issues = JSON.parse(
    run([
      "issue",
      "list",
      "--repo",
      repository,
      "--state",
      "all",
      "--limit",
      String(issueLimit),
      "--search",
      `in:title \"${reportTitle}\"`,
      "--json",
      "number,title,body,author,state,labels",
    ]),
  );
  const plan = planPublication(snapshot, issues, options);
  if (plan.action === "none") return plan;
  if (plan.number) {
    const previous = issues.find((issue) => issue.number === plan.number);
    const current = JSON.parse(
      run([
        "issue",
        "view",
        String(plan.number),
        "--repo",
        repository,
        "--json",
        "number,title,body,author,state,labels",
      ]),
    );
    const rechecked = planPublication(snapshot, [current], options);
    if (
      current.body !== previous.body ||
      current.state !== previous.state ||
      JSON.stringify(rechecked) !== JSON.stringify(plan)
    ) {
      throw new Error(
        "Generated issue changed during publication; retry after maintainer review",
      );
    }
  }
  if (plan.action === "close") {
    run([
      "issue",
      "close",
      String(plan.number),
      "--repo",
      repository,
      "--reason",
      "completed",
    ]);
    return plan;
  }
  const directory = mkdtempSync(
    path.join(os.tmpdir(), "aipermission-major-report-"),
  );
  try {
    const bodyFile = path.join(directory, "report.md");
    writeFileSync(bodyFile, plan.body);
    if (plan.action === "create")
      run([
        "issue",
        "create",
        "--repo",
        repository,
        "--title",
        reportTitle,
        "--body-file",
        bodyFile,
        "--label",
        "maintenance",
      ]);
    else
      run([
        "issue",
        "edit",
        String(plan.number),
        "--repo",
        repository,
        "--body-file",
        bodyFile,
      ]);
    if (plan.action === "reopen")
      run(["issue", "reopen", String(plan.number), "--repo", repository]);
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
  return plan;
}

function main() {
  const { values } = parseArgs({
    options: {
      report: { type: "string" },
      repo: { type: "string" },
      "adopt-issue": { type: "string" },
    },
  });
  if (!values.report) throw new Error("--report is required");
  const source = readFileSync(values.report, "utf8");
  if (Buffer.byteLength(source) > 1_000_000)
    throw new Error("Dependency report snapshot is too large");
  const adoptIssueNumber = values["adopt-issue"]
    ? Number(values["adopt-issue"])
    : 0;
  if (!Number.isSafeInteger(adoptIssueNumber) || adoptIssueNumber < 0)
    throw new Error("Invalid adoption issue number");
  const plan = publishReport(JSON.parse(source), values.repo, runGitHub, {
    adoptIssueNumber,
  });
  process.stdout.write(`Dependency report: ${plan.action}\n`);
}

if (require.main === module) main();

module.exports = { planPublication, publishReport, reportTitle };
