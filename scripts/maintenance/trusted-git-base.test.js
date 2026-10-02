const assert = require("node:assert/strict");
const path = require("node:path");
const test = require("node:test");
const { temporaryRoot } = require("./test-fixtures");
const { eventBase, resolveTrustedBase } = require("../trusted-git-base");

const base = "a".repeat(40),
  head = "b".repeat(40);

function withGitHubEvent(t, eventName, event) {
  const directory = temporaryRoot(t, { "event.json": JSON.stringify(event) });
  const eventPath = path.join(directory, "event.json");
  const names = ["GITHUB_ACTIONS", "GITHUB_EVENT_NAME", "GITHUB_EVENT_PATH"];
  const previous = Object.fromEntries(names.map((name) => [name, process.env[name]]));
  Object.assign(process.env, {
    GITHUB_ACTIONS: "true",
    GITHUB_EVENT_NAME: eventName,
    GITHUB_EVENT_PATH: eventPath,
  });
  t.after(() => {
    for (const [key, value] of Object.entries(previous))
      if (value === undefined) delete process.env[key];
      else process.env[key] = value;
  });
}

function fakeGit(responses) {
  return (...args) => {
    const key = args.join(" ");
    if (Object.hasOwn(responses, key)) return responses[key];
    throw new Error(`unexpected git command: ${key}`);
  };
}

function resolveBase(configured, gitCommand, environment) {
  return resolveTrustedBase({
    configured,
    gitCommand,
    environment,
    variable: "POLICY_BASE",
    root: ".",
  });
}

test("GitHub ratchets derive their base from the event payload", (t) => {
  withGitHubEvent(t, "pull_request", { pull_request: { base: { sha: base } } });
  assert.equal(eventBase(), base);
  const gitCommand = fakeGit({
    [`rev-parse ${base}^{commit}`]: base,
    "rev-parse HEAD^{commit}": head,
    [`merge-base --is-ancestor ${base} ${head}`]: "",
  });
  assert.equal(resolveBase("HEAD", gitCommand), base);
});

test("GitHub ratchets reject a zero push predecessor", (t) => {
  withGitHubEvent(t, "push", { before: "0".repeat(40) });
  assert.throws(() => resolveBase("ignored"), /non-zero base commit/);
});

test("workflow dispatch accepts only an explicit immutable base commit", (t) => {
  withGitHubEvent(t, "workflow_dispatch", {});
  const gitCommand = fakeGit({
    [`rev-parse ${base}^{commit}`]: base,
    "rev-parse HEAD^{commit}": head,
    [`merge-base --is-ancestor ${base} ${head}`]: "",
  });
  assert.equal(resolveBase(base, gitCommand), base);
  assert.throws(() => resolveBase("", gitCommand), /immutable workflow_dispatch base commit/);
});

test("local ratchets ignore inherited GitHub Actions context", () => {
  const gitCommand = fakeGit({
    "merge-base HEAD origin/main": head,
    [`rev-parse ${head}^{commit}`]: head,
    "rev-parse HEAD^{commit}": head,
    "rev-parse HEAD^^{commit}": base,
    [`merge-base --is-ancestor ${base} ${head}`]: "",
  });
  assert.equal(resolveBase("", gitCommand, {}), base);
});
