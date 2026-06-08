#!/usr/bin/env node
const { execFileSync } = require("node:child_process");
const { readFileSync } = require("node:fs");

const tracked = execFileSync("git", ["ls-files", "-z"], { encoding: "utf8" })
  .split("\0")
  .filter(Boolean);

function isShellLike(file, content) {
  if (file.endsWith(".sh")) return true;
  const firstLineEnd = content.indexOf("\n");
  const firstLine = content.slice(0, firstLineEnd === -1 ? content.length : firstLineEnd);
  return firstLine.startsWith("#!") && /\b(sh|bash|zsh|dash)\b/.test(firstLine);
}

const findings = [];
let checked = 0;

for (const file of tracked) {
  let content;
  try {
    content = readFileSync(file, "utf8");
  } catch {
    continue;
  }
  if (!isShellLike(file, content)) continue;
  checked += 1;
  if (content.includes("\r\n") || /\r(?!\n)/.test(content)) {
    findings.push(file);
  }
}

if (findings.length > 0) {
  console.error("Line-ending check failed. Shell scripts must use LF line endings:");
  for (const finding of findings) console.error(`- ${finding}`);
  console.error("\nRun: git add --renormalize <file>");
  process.exit(1);
}

console.log(`Line-ending check passed (${checked} shell-like files checked).`);
