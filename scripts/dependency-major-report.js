#!/usr/bin/env node

const { execFileSync } = require("node:child_process");
const { readFileSync, writeFileSync } = require("node:fs");
const path = require("node:path");

const root = path.resolve(__dirname, "..");
const npmDirectories = ["frontend", "packages/mcp"];

function commandOutput(command, args, cwd, acceptJSONFailure = false) {
  try {
    return execFileSync(command, args, { cwd, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] });
  } catch (error) {
    const stdout = typeof error.stdout === "string" ? error.stdout.trim() : "";
    if (acceptJSONFailure && stdout) {
      JSON.parse(stdout);
      return stdout;
    }
    const stderr = typeof error.stderr === "string" ? error.stderr.trim() : "";
    throw new Error(`${command} failed${stderr ? `: ${stderr}` : ""}`, { cause: error });
  }
}

function major(value) {
  const match = String(value || "").match(/^v?(\d+)\./);
  return match ? Number(match[1]) : null;
}

function npmMajorUpdates(directory) {
  const raw = commandOutput("npm", ["outdated", "--json", "--workspaces=false"], path.join(root, directory), true).trim();
  if (!raw) return [];
  const report = JSON.parse(raw);
  return Object.entries(report)
    .filter(([, item]) => major(item.latest) !== null && major(item.current) !== null && major(item.latest) > major(item.current))
    .map(([name, item]) => ({ ecosystem: "npm", directory, name, current: item.current, latest: item.latest }));
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
  const format = "{{if .Update}}{{.Path}}\t{{.Version}}\t{{.Update.Version}}{{end}}";
  const raw = commandOutput("go", ["list", "-m", "-u", "-f", format, "all"], path.join(root, "backend"));
  return raw
    .split("\n")
    .filter(Boolean)
    .map((line) => line.split("\t"))
    .filter(([name, current, latest]) => direct.has(name) && major(latest) !== null && major(current) !== null && major(latest) > major(current))
    .map(([name, current, latest]) => ({ ecosystem: "Go", directory: "backend", name, current, latest }));
}

function renderReport(updates) {
  const lines = [
    "# Deferred Major Dependency Updates",
    "",
    "This scheduled report is informational. Major upgrades require maintainer review and never create or merge bot-authored commits.",
    "",
  ];
  if (updates.length === 0) {
    lines.push("No direct Go or npm major updates are currently available.");
  } else {
    lines.push("| Ecosystem | Directory | Dependency | Current | Latest |", "| --- | --- | --- | --- | --- |");
    for (const item of updates.sort((left, right) => `${left.ecosystem}:${left.name}`.localeCompare(`${right.ecosystem}:${right.name}`))) {
      lines.push(`| ${item.ecosystem} | \`${item.directory}\` | \`${item.name}\` | \`${item.current}\` | \`${item.latest}\` |`);
    }
  }
  lines.push("", "Docker base images, GitHub Actions, and native dependencies remain separately pinned and manually reviewed.", "");
  lines.push("Go detection covers updates reported on an existing module path. Major versions that require a new `/vN` module path remain a manual maintainer review.", "");
  return lines.join("\n");
}

function main() {
  const outputIndex = process.argv.indexOf("--output");
  const outputPath = outputIndex >= 0 ? process.argv[outputIndex + 1] : "";
  const updates = [...npmDirectories.flatMap(npmMajorUpdates), ...goMajorUpdates()];
  const report = renderReport(updates);
  if (outputPath) writeFileSync(outputPath, report);
  else process.stdout.write(report);
}

if (require.main === module) main();

module.exports = { directGoModules, major, renderReport };
