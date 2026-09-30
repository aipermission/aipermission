#!/usr/bin/env node

const assert = require("node:assert/strict");
const {
  existsSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
} = require("node:fs");
const { spawnSync } = require("node:child_process");
const os = require("node:os");
const path = require("node:path");
const {
  parseNpmMajorUpdates,
  renderReport,
  reportSnapshot,
} = require("../dependency-major-report");
const {
  planPublication,
  publishReport,
  reportTitle,
} = require("../dependency-major-publish");

assert.equal(reportSnapshot([]).schema_version, 1);
assert.deepEqual(reportSnapshot([]).updates, []);

const updates = [
  {
    ecosystem: "npm",
    directory: "frontend",
    name: "example",
    current: "1.4.0",
    latest: "2.0.0",
  },
];
const snapshot = reportSnapshot(updates);
const empty = reportSnapshot([]);
const issue = (overrides = {}) => ({
  number: 193,
  title: reportTitle,
  body: renderReport(updates),
  state: "OPEN",
  labels: [{ name: "maintenance" }],
  author: { login: "app/github-actions" },
  ...overrides,
});

assert.deepEqual(planPublication(empty, []), { action: "none" });
assert.deepEqual(planPublication(empty, [issue()]), {
  action: "close",
  number: 193,
});
assert.deepEqual(planPublication(empty, [issue({ state: "CLOSED" })]), {
  action: "none",
});
assert.deepEqual(planPublication(snapshot, [issue()]), { action: "none" });
assert.equal(planPublication(snapshot, []).action, "create");
assert.equal(
  planPublication(snapshot, [issue({ body: renderReport([]) })]).action,
  "update",
);
assert.equal(
  planPublication(snapshot, [issue({ state: "CLOSED" })]).action,
  "reopen",
);
assert.equal(
  planPublication(snapshot, [
    issue({ state: "CLOSED", number: 192 }),
    issue({ state: "CLOSED" }),
  ]).number,
  193,
);
assert.deepEqual(
  planPublication(
    empty,
    [issue({ body: renderReport([], { includeMarker: false }) })],
    { adoptIssueNumber: 193 },
  ),
  { action: "close", number: 193 },
);
assert.throws(
  () =>
    planPublication(empty, [
      issue({ body: renderReport([], { includeMarker: false }) }),
    ]),
  /ownership is ambiguous/,
);
assert.equal(
  planPublication(snapshot, [
    issue({ number: 180, state: "CLOSED", body: "Historical edited report" }),
    issue(),
  ]).action,
  "none",
);
assert.equal(
  planPublication(snapshot, [
    issue({ state: "CLOSED", body: "Historical edited report" }),
  ]).action,
  "create",
);
assert.throws(
  () =>
    planPublication(
      empty,
      [
        issue({
          number: 180,
          body: renderReport([], { includeMarker: false }),
        }),
      ],
      { adoptIssueNumber: 193 },
    ),
  /ownership is ambiguous/,
);
for (const overrides of [
  { title: "Similar report" },
  { author: { login: "maintainer" } },
  { labels: [] },
]) {
  assert.deepEqual(planPublication(empty, [issue(overrides)]), {
    action: "none",
  });
}
assert.throws(
  () => planPublication(snapshot, [issue({ body: "Manually edited report" })]),
  /ownership is ambiguous/,
);
for (const scan of [snapshot, empty])
  assert.throws(
    () =>
      planPublication(scan, [
        issue({
          body: renderReport(updates).replace(
            "<!-- aipermission:deferred-major-report-sha256:",
            "Human follow-up\n<!-- aipermission:deferred-major-report-sha256:",
          ),
        }),
      ]),
    /ownership is ambiguous/,
  );
assert.throws(
  () =>
    planPublication(snapshot, [
      issue({ body: `${renderReport(updates)}Human follow-up\n` }),
    ]),
  /ownership is ambiguous/,
);
assert.throws(
  () => planPublication(snapshot, [issue(), issue({ number: 194 })]),
  /Multiple/,
);
assert.throws(
  () => planPublication(snapshot, Array(100).fill(issue())),
  /incomplete/,
);
assert.throws(
  () => planPublication({ schema_version: 2, updates: [] }, []),
  /Invalid/,
);
for (const update of [
  { ...updates[0], latest: "1.5.0" },
  { ...updates[0], name: "evil | row" },
  { ...updates[0], directory: "unknown" },
]) {
  assert.throws(() => planPublication(reportSnapshot([update]), []), /Invalid/);
}
assert.throws(
  () => planPublication(reportSnapshot([...updates, ...updates]), []),
  /Duplicate/,
);
assert.deepEqual(parseNpmMajorUpdates("{}", "frontend"), []);
assert.deepEqual(
  parseNpmMajorUpdates(
    '{"example":{"current":"1.4.0","latest":"2.0.0"}}',
    "frontend",
  ),
  updates,
);
for (const raw of [
  '{"error":{"code":"E503"}}',
  "[]",
  "null",
  '{"example":{"latest":"2.0.0"}}',
]) {
  assert.throws(
    () => parseNpmMajorUpdates(raw, "frontend"),
    /Invalid|Incomplete/,
  );
}
const beforeRender = JSON.stringify(updates);
renderReport(updates);
assert.equal(
  JSON.stringify(updates),
  beforeRender,
  "rendering must not mutate the snapshot",
);

// Drive real publication argument construction against an in-memory issue store, never GitHub.
let stored = [];
const writes = [];
const temporaryFiles = [];
function fakeGitHub(args) {
  assert.equal(args[0], "issue");
  assert.equal(args[args.indexOf("--repo") + 1], "owner/repository");
  const verb = args[1];
  if (verb === "list") return JSON.stringify(stored);
  if (verb === "view") return JSON.stringify(stored[0]);
  writes.push(verb);
  const bodyIndex = args.indexOf("--body-file");
  const bodyFile = bodyIndex < 0 ? null : args[bodyIndex + 1];
  const body = bodyFile ? readFileSync(bodyFile, "utf8") : null;
  if (bodyFile) temporaryFiles.push(bodyFile);
  if (verb === "create") stored = [issue({ body })];
  else if (verb === "edit") stored[0].body = body;
  else if (verb === "close") stored[0].state = "CLOSED";
  else if (verb === "reopen") stored[0].state = "OPEN";
  else assert.fail(`Unexpected GitHub command ${verb}`);
  return "";
}
assert.equal(
  publishReport(empty, "owner/repository", fakeGitHub).action,
  "none",
);
assert.equal(
  publishReport(snapshot, "owner/repository", fakeGitHub).action,
  "create",
);
assert.equal(
  publishReport(snapshot, "owner/repository", fakeGitHub).action,
  "none",
);
const changed = reportSnapshot([{ ...updates[0], latest: "3.0.0" }]);
assert.equal(
  publishReport(changed, "owner/repository", fakeGitHub).action,
  "update",
);
assert.equal(
  publishReport(empty, "owner/repository", fakeGitHub).action,
  "close",
);
assert.equal(
  publishReport(empty, "owner/repository", fakeGitHub).action,
  "none",
);
assert.equal(
  publishReport(snapshot, "owner/repository", fakeGitHub).action,
  "reopen",
);
assert.deepEqual(writes, ["create", "edit", "close", "edit", "reopen"]);
assert.ok(
  temporaryFiles.every((file) => !existsSync(file)),
  "temporary report files must be removed",
);
assert.equal(stored.length, 1, "reuse the same issue after a cleared report");

const failureWrites = [];
assert.throws(
  () =>
    publishReport(empty, "owner/repository", () => {
      throw new Error("GitHub search failed");
    }),
  /search failed/,
);
assert.throws(() =>
  publishReport(empty, "owner/repository", () => "{not-json}"),
);
assert.throws(
  () =>
    publishReport({ updates: [] }, "owner/repository", () =>
      assert.fail("invalid scan must not call GitHub"),
    ),
  /Invalid/,
);
assert.throws(
  () =>
    publishReport(snapshot, "owner/repository", (args) => {
      if (args[1] === "list")
        return JSON.stringify([issue({ state: "CLOSED" })]);
      if (args[1] === "view") return JSON.stringify(issue({ state: "CLOSED" }));
      failureWrites.push(args[1]);
      const file = args[args.indexOf("--body-file") + 1];
      temporaryFiles.push(file);
      throw new Error("GitHub edit failed");
    }),
  /edit failed/,
);
assert.deepEqual(
  failureWrites,
  ["edit"],
  "a failed body update must not reopen a stale issue",
);
assert.ok(
  temporaryFiles.every((file) => !existsSync(file)),
  "failure must clean temporary report files",
);
for (const scan of [empty, changed]) {
  let calls = 0;
  assert.throws(
    () =>
      publishReport(scan, "owner/repository", (args) => {
        calls += 1;
        if (args[1] === "list") return JSON.stringify([issue()]);
        if (args[1] === "view")
          return JSON.stringify(
            issue({
              body: renderReport(updates).replace(
                "<!-- aipermission:deferred-major-report-sha256:",
                "Concurrent human note\n<!-- aipermission:deferred-major-report-sha256:",
              ),
            }),
          );
        assert.fail("changed issue must not be edited or closed");
      }),
    /ownership is ambiguous|changed during publication/,
  );
  assert.equal(calls, 2);
}

// Exercise the actual scanner process with disposable npm/go executables, not network queries.
const fixture = mkdtempSync(path.join(os.tmpdir(), "aipermission-major-cli-"));
try {
  const fakeCommand = `#!${process.execPath}\nconst path = require("node:path");
if (path.basename(process.argv[1]) === "npm") {
  process.stdout.write(process.env.TEST_NPM_STDOUT);
  process.stderr.write(process.env.TEST_NPM_STDERR);
  process.exit(Number(process.env.TEST_NPM_STATUS));
}\n`;
  for (const name of ["npm", "go"])
    writeFileSync(path.join(fixture, name), fakeCommand, { mode: 0o700 });
  const scanner = path.resolve(__dirname, "../dependency-major-report.js");
  const cases = [
    { name: "empty", stdout: "{}", status: 0, ok: true },
    { name: "empty-nonzero", stdout: "{}", status: 1 },
    {
      name: "major",
      stdout: '{"example":{"current":"1.4.0","latest":"2.0.0"}}',
      status: 1,
      ok: true,
    },
    {
      name: "minor",
      stdout: '{"example":{"current":"1.4.0","latest":"1.5.0"}}',
      status: 1,
      ok: true,
    },
    {
      name: "warning",
      stdout: '{"example":{"current":"1.4.0","latest":"2.0.0"}}',
      stderr: "npm warn Ignoring workspaces\n",
      status: 1,
      ok: true,
    },
    { name: "error-object", stdout: '{"error":{"code":"E503"}}', status: 1 },
    {
      name: "empty-error",
      stdout: "{}",
      stderr: "npm error E503\n",
      status: 1,
    },
    {
      name: "partial-error",
      stdout: '{"example":{"current":"1.4.0","latest":"2.0.0"}}',
      stderr: "npm error registry unavailable\n",
      status: 1,
    },
    { name: "wrong-exit", stdout: "{}", status: 2 },
    { name: "incomplete", stdout: '{"example":{"latest":"2.0.0"}}', status: 1 },
    { name: "array", stdout: "[]", status: 1 },
    { name: "invalid-json", stdout: "not-json", status: 1 },
  ];
  for (const entry of cases) {
    const output = path.join(fixture, `${entry.name}.json`);
    const result = spawnSync(
      process.execPath,
      [scanner, "--json-output", output],
      {
        encoding: "utf8",
        timeout: 10_000,
        env: {
          ...process.env,
          PATH: fixture,
          TEST_NPM_STDOUT: entry.stdout,
          TEST_NPM_STDERR: entry.stderr || "",
          TEST_NPM_STATUS: String(entry.status),
        },
      },
    );
    assert.ifError(result.error);
    assert.equal(
      result.status === 0,
      Boolean(entry.ok),
      `${entry.name}: ${result.stderr}`,
    );
    assert.equal(
      existsSync(output),
      Boolean(entry.ok),
      `${entry.name}: failed scan must not publish an artifact`,
    );
    if (entry.ok)
      assert.equal(JSON.parse(readFileSync(output, "utf8")).schema_version, 1);
  }
} finally {
  rmSync(fixture, { recursive: true, force: true });
}

console.log("Dependency major publication tests passed.");
