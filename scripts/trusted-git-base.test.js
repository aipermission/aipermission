const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const test = require("node:test");
const { eventBase, resolveTrustedBase } = require("./trusted-git-base");

function withGitHubEvent(t, eventName, event) {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "aipermission-event-"));
  const eventPath = path.join(directory, "event.json");
  fs.writeFileSync(eventPath, JSON.stringify(event));
  const names = ["GITHUB_ACTIONS", "GITHUB_EVENT_NAME", "GITHUB_EVENT_PATH"];
  const previous = Object.fromEntries(names.map((name) => [name, process.env[name]]));
  Object.assign(process.env, { GITHUB_ACTIONS: "true", GITHUB_EVENT_NAME: eventName, GITHUB_EVENT_PATH: eventPath });
  t.after(() => {
    for (const [key, value] of Object.entries(previous))
      if (value === undefined) delete process.env[key]; else process.env[key] = value;
    fs.rmSync(directory, { recursive: true, force: true });
  });
}

function fakeGit(responses) {
  return (...args) => {
    const key = args.join(" ");
    if (Object.hasOwn(responses, key)) return responses[key];
    throw new Error(`unexpected git command: ${key}`);
  };
}

test("GitHub ratchets derive their base from the event payload", (t) => {
  const base = "a".repeat(40), head = "b".repeat(40);
  withGitHubEvent(t, "pull_request", { pull_request: { base: { sha: base } } });
  assert.equal(eventBase(), base);
  const gitCommand = fakeGit({ [`rev-parse ${base}^{commit}`]: base, "rev-parse HEAD^{commit}": head, [`merge-base --is-ancestor ${base} ${head}`]: "" });
  assert.equal(resolveTrustedBase({ configured: "HEAD", variable: "POLICY_BASE", root: ".", gitCommand }), base);
});

test("GitHub ratchets reject a zero push predecessor", (t) => {
  withGitHubEvent(t, "push", { before: "0".repeat(40) });
  assert.throws(() => resolveTrustedBase({ configured: "ignored", variable: "POLICY_BASE", root: "." }), /non-zero base commit/);
});

test("local ratchets use HEAD parent when merge base is HEAD", () => {
  const head = "b".repeat(40), parent = "a".repeat(40);
  const gitCommand = fakeGit({
    "merge-base HEAD origin/main": head, [`rev-parse ${head}^{commit}`]: head,
    "rev-parse HEAD^{commit}": head, "rev-parse HEAD^^{commit}": parent,
    [`merge-base --is-ancestor ${parent} ${head}`]: "",
  });
  assert.equal(resolveTrustedBase({ configured: "", variable: "POLICY_BASE", root: ".", gitCommand }), parent);
});
