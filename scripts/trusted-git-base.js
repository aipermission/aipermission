const fs = require("node:fs");
const { execFileSync } = require("node:child_process");

function git(root, ...args) {
  return execFileSync("git", args, {
    cwd: root,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
  }).trim();
}

function eventBase() {
  const eventPath = process.env.GITHUB_EVENT_PATH;
  if (process.env.GITHUB_ACTIONS !== "true" || !eventPath) return "";
  const event = JSON.parse(fs.readFileSync(eventPath, "utf8"));
  if (process.env.GITHUB_EVENT_NAME === "pull_request") {
    return String(event.pull_request?.base?.sha || "").trim();
  }
  if (process.env.GITHUB_EVENT_NAME === "push") {
    return String(event.before || "").trim();
  }
  throw new Error(
    `unsupported GitHub event for policy ratchet: ${process.env.GITHUB_EVENT_NAME || "missing"}`,
  );
}

function resolveTrustedBase({
  configured,
  variable,
  root,
  gitCommand = (...args) => git(root, ...args),
}) {
  const inGitHub = process.env.GITHUB_ACTIONS === "true";
  const configuredReference = String(configured || "").trim();
  let reference = inGitHub ? eventBase() : configuredReference;
  if (!reference) reference = gitCommand("merge-base", "HEAD", "origin/main");
  if (!reference || /^0+$/.test(reference)) {
    throw new Error(`${variable} must identify a non-zero base commit`);
  }
  if (inGitHub && !/^[0-9a-f]{40}$/i.test(reference)) {
    throw new Error(`${variable} event base must be a full commit SHA`);
  }
  let baseCommit = gitCommand("rev-parse", `${reference}^{commit}`);
  const headCommit = gitCommand("rev-parse", "HEAD^{commit}");
  if (!inGitHub && !configuredReference && baseCommit === headCommit) {
    reference = "HEAD^";
    baseCommit = gitCommand("rev-parse", `${reference}^{commit}`);
  }
  if (baseCommit === headCommit) {
    throw new Error(`${variable} must not resolve to HEAD`);
  }
  try {
    gitCommand("merge-base", "--is-ancestor", baseCommit, headCommit);
  } catch {
    throw new Error(`${variable} ${baseCommit} is not an ancestor of HEAD`);
  }
  return baseCommit;
}

module.exports = { eventBase, resolveTrustedBase };
