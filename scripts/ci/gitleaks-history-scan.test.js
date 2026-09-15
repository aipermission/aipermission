const assert = require("node:assert/strict");
const childProcess = require("node:child_process");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const test = require("node:test");
const { workflowJobContracts } = require("../workflow-contracts");

const script = path.join(__dirname, "..", "gitleaks-history-scan.sh");
const config = path.join(__dirname, "../..", ".gitleaks.toml");

test("history scan fails closed when the container cannot enumerate commits", () => {
  for (const fixture of [
    { mode: "error", message: "mounted Git history is unreadable" },
    {
      mode: "shallow",
      message: "shallow repositories cannot prove full-history coverage",
    },
    {
      mode: "invalid-depth",
      message: "mounted repository depth is invalid",
    },
    { mode: "zero", message: "mounted repository contains no commits" },
    { mode: "malformed", message: "mounted commit count is invalid" },
  ]) {
    const result = runWithFakeDocker(fixture.mode);
    assert.notEqual(result.status, 0, fixture.mode);
    assert.match(result.stderr, new RegExp(fixture.message), fixture.mode);
    assert.equal(
      result.calls.filter((call) => call.includes("detect --source=/repo"))
        .length,
      0,
      fixture.mode,
    );
  }
});

test("history scan runs detection only after a non-empty mounted history preflight", () => {
  const result = runWithFakeDocker("valid");
  assert.equal(result.status, 0, result.stderr);
  const calls = result.calls;
  assert.equal(calls.length, 3);
  assert.match(
    calls[0],
    /--entrypoint git .* rev-parse --is-shallow-repository/,
  );
  assert.match(calls[1], /--entrypoint git .* rev-list --all --count/);
  assert.match(calls[2], /:ro .* detect --source=\/repo/);
});

test("security hygiene checkout fetches complete history", () => {
  const workflowPath = path.join(__dirname, "../..", ".github/workflows/ci.yml");
  const jobs = workflowJobContracts(
    fs.readFileSync(workflowPath, "utf8"),
    ".github/workflows/ci.yml",
  );
  const checkout = jobs
    .get("security-hygiene")
    .source.steps.find((step) => step.uses?.startsWith("actions/checkout@"));
  assert.equal(checkout?.with?.["fetch-depth"], 0);
});

test("history scan allowlist is limited to generated recipe digests", () => {
  const source = fs.readFileSync(config, "utf8");
  assert.match(source, /\[extend\]\s+useDefault = true/);
  assert.match(source, /id = "generic-api-key"/);
  assert.match(source, /regexTarget = "secret"/);
  assert.match(source, /\^\[a-f0-9\]\{64\}\$/);
  assert.match(
    source,
    /\^scripts\/verification-policy\\\.json\$/,
  );
  assert.doesNotMatch(source, /commits\s*=/);
});

function runWithFakeDocker(mode) {
  const directory = fs.mkdtempSync(
    path.join(os.tmpdir(), "aipermission-history-scan-"),
  );
  const fakeDocker = path.join(directory, "docker");
  const log = path.join(directory, "calls.log");
  fs.writeFileSync(
    fakeDocker,
    `#!/bin/sh
printf '%s\\n' "$*" >> "$AIPERMISSION_HISTORY_TEST_LOG"
case "$*" in
  *"rev-parse --is-shallow-repository"*)
    case "$AIPERMISSION_HISTORY_TEST_MODE" in
      error) echo "fatal: not a repository" >&2; exit 128 ;;
      shallow) printf 'true\n' ;;
      invalid-depth) printf 'unknown\n' ;;
      *) printf 'false\n' ;;
    esac
    ;;
  *"rev-list --all --count"*)
    case "$AIPERMISSION_HISTORY_TEST_MODE" in
      error) echo "fatal: bad object refs/heads/dev" >&2; exit 128 ;;
      zero) printf '0\\n' ;;
      malformed) printf 'not-a-count\\n' ;;
      valid) printf '42\\n' ;;
    esac
    ;;
  *"detect --source=/repo"*) exit 0 ;;
  *) echo "unexpected docker invocation: $*" >&2; exit 99 ;;
esac
`,
    { mode: 0o700 },
  );
  const result = childProcess.spawnSync("sh", [script], {
    cwd: path.join(__dirname, "../.."),
    encoding: "utf8",
    env: {
      ...process.env,
      PATH: `${directory}${path.delimiter}${process.env.PATH}`,
      AIPERMISSION_HISTORY_TEST_LOG: log,
      AIPERMISSION_HISTORY_TEST_MODE: mode,
    },
  });
  const calls = readCalls(log);
  fs.rmSync(directory, { recursive: true, force: true });
  return { ...result, calls };
}

function readCalls(log) {
  if (!fs.existsSync(log)) return [];
  return fs.readFileSync(log, "utf8").trim().split("\n").filter(Boolean);
}
