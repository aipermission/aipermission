#!/usr/bin/env node

const { execFileSync } = require("node:child_process");
const { createHash } = require("node:crypto");
const { readFileSync, writeFileSync } = require("node:fs");
const path = require("node:path");
const { parseArgs } = require("node:util");

const root = path.resolve(__dirname, "..");
const npmDirectories = ["frontend", "packages/mcp", "scripts"];
const reportMarker = "<!-- aipermission:deferred-major-report:v1 -->";

function commandOutput(command, args, cwd, acceptJSONFailure = false) {
  try {
    return execFileSync(command, args, {
      cwd,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "pipe"],
    });
  } catch (error) {
    const stdout = typeof error.stdout === "string" ? error.stdout.trim() : "";
    const stderr = typeof error.stderr === "string" ? error.stderr.trim() : "";
    if (acceptJSONFailure && error.status === 1 && stdout) {
      const parsed = JSON.parse(stdout);
      if (
        !parsed ||
        Array.isArray(parsed) ||
        typeof parsed !== "object" ||
        parsed.error ||
        Object.keys(parsed).length === 0 ||
        (stderr &&
          stderr.split("\n").some((line) => !/^npm warn\b/i.test(line)))
      )
        throw error;
      return stdout;
    }
    throw new Error(`${command} failed${stderr ? `: ${stderr}` : ""}`, {
      cause: error,
    });
  }
}

function major(value) {
  const match = String(value || "").match(/^v?(\d+)\./);
  return match ? Number(match[1]) : null;
}

function npmMajorUpdates(directory) {
  const raw = commandOutput(
    "npm",
    ["outdated", "--json", "--workspaces=false"],
    path.join(root, directory),
    true,
  ).trim();
  if (!raw) return [];
  return parseNpmMajorUpdates(raw, directory);
}

function parseNpmMajorUpdates(raw, directory) {
  const report = JSON.parse(raw);
  if (
    !report ||
    typeof report !== "object" ||
    Array.isArray(report) ||
    report.error
  )
    throw new Error("Invalid npm outdated response");
  for (const item of Object.values(report)) {
    if (!item || major(item.current) === null || major(item.latest) === null)
      throw new Error("Incomplete npm outdated response");
  }
  return Object.entries(report)
    .filter(
      ([, item]) =>
        major(item.latest) !== null &&
        major(item.current) !== null &&
        major(item.latest) > major(item.current),
    )
    .map(([name, item]) => ({
      ecosystem: "npm",
      directory,
      name,
      current: item.current,
      latest: item.latest,
    }));
}

function directGoModules() {
  const source = readFileSync(path.join(root, "backend", "go.mod"), "utf8");
  const modules = new Set();
  let inBlock = false;
  for (const rawLine of source.split("\n")) {
    const line = rawLine.trim();
    if (line === "require (") {
      inBlock = true;
      continue;
    }
    if (inBlock && line === ")") {
      inBlock = false;
      continue;
    }
    if (!inBlock || !line || line.includes("// indirect")) continue;
    const [name] = line.split(/\s+/);
    if (name) modules.add(name);
  }
  return modules;
}

function goMajorUpdates() {
  const direct = directGoModules();
  const format =
    "{{if .Update}}{{.Path}}\t{{.Version}}\t{{.Update.Version}}{{end}}";
  const raw = commandOutput(
    "go",
    ["list", "-m", "-u", "-f", format, "all"],
    path.join(root, "backend"),
  );
  return raw
    .split("\n")
    .filter(Boolean)
    .map((line) => line.split("\t"))
    .filter(
      ([name, current, latest]) =>
        direct.has(name) &&
        major(latest) !== null &&
        major(current) !== null &&
        major(latest) > major(current),
    )
    .map(([name, current, latest]) => ({
      ecosystem: "Go",
      directory: "backend",
      name,
      current,
      latest,
    }));
}

function renderReport(updates, { includeMarker = true } = {}) {
  const lines = [
    "# Deferred Major Dependency Updates",
    "",
    "This scheduled report is informational. Major upgrades require maintainer review and never create or merge bot-authored commits.",
    "",
  ];
  if (updates.length === 0) {
    lines.push("No direct Go or npm major updates are currently available.");
  } else {
    lines.push(
      "| Ecosystem | Directory | Dependency | Current | Latest |",
      "| --- | --- | --- | --- | --- |",
    );
    for (const item of [...updates].sort((left, right) =>
      `${left.ecosystem}:${left.directory}:${left.name}`.localeCompare(
        `${right.ecosystem}:${right.directory}:${right.name}`,
      ),
    )) {
      lines.push(
        `| ${item.ecosystem} | \`${item.directory}\` | \`${item.name}\` | \`${item.current}\` | \`${item.latest}\` |`,
      );
    }
  }
  lines.push(
    "",
    "Docker base images, GitHub Actions, and native dependencies remain separately pinned and manually reviewed.",
    "",
  );
  lines.push(
    "Go detection covers updates reported on an existing module path. Major versions that require a new `/vN` module path remain a manual maintainer review.",
    "",
  );
  const body = lines.join("\n");
  if (!includeMarker) return body;
  const digest = createHash("sha256").update(body).digest("hex");
  return `${body}<!-- aipermission:deferred-major-report-sha256:${digest} -->\n${reportMarker}\n`;
}

function isGeneratedReport(body) {
  if (typeof body !== "string") return false;
  const trailer =
    /<!-- aipermission:deferred-major-report-sha256:([a-f0-9]{64}) -->\n<!-- aipermission:deferred-major-report:v1 -->\n$/.exec(
      body,
    );
  return Boolean(
    trailer &&
    createHash("sha256").update(body.slice(0, trailer.index)).digest("hex") ===
      trailer[1],
  );
}

function reportSnapshot(updates) {
  return { schema_version: 1, updates };
}

function main() {
  const { values } = parseArgs({
    options: { output: { type: "string" }, "json-output": { type: "string" } },
  });
  const updates = [
    ...npmDirectories.flatMap(npmMajorUpdates),
    ...goMajorUpdates(),
  ];
  const report = renderReport(updates);
  if (values.output) writeFileSync(values.output, report);
  if (values["json-output"])
    writeFileSync(
      values["json-output"],
      `${JSON.stringify(reportSnapshot(updates), null, 2)}\n`,
    );
  if (!values.output && !values["json-output"]) process.stdout.write(report);
}

if (require.main === module) main();

module.exports = {
  directGoModules,
  major,
  isGeneratedReport,
  parseNpmMajorUpdates,
  renderReport,
  reportMarker,
  reportSnapshot,
};
