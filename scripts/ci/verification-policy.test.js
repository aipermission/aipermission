const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");
const {
  loadPolicy,
  validateRequiredCheckMigrations,
  verifyActionPinsInSource,
  verifyNoRemovals,
  verifyWorkflows,
  workflowJobs,
} = require("../verification-policy");

const fixture = path.join(__dirname, "../..", "fixture.yml");
const gatePolicy = {
  required_checks: [
    {
      name: "Gate",
      workflow: "fixture.yml",
      job: "gate",
      job_name: "Gate",
      commands: ["node verify.js"],
    },
  ],
};
const workflow = (steps, extra = "") =>
  `name: Fixture\njobs:\n  gate:\n    name: Gate\n    steps:\n${steps}\n${extra}`;

function useFixture(t) {
  t.after(() => fs.rmSync(fixture, { force: true }));
  return (source) => fs.writeFileSync(fixture, source);
}

test("required verification policy matches workflow jobs and commands", () => {
  assert.doesNotThrow(() => verifyWorkflows());
});

test("workflow parser scopes commands to their owning job", () => {
  const jobs = workflowJobs(
    "name: CI\njobs:\n  first:\n    steps:\n      - run: alpha\n  second:\n    steps:\n      - run: beta\n",
  );
  assert.match(jobs.get("first"), /alpha/);
  assert.doesNotMatch(jobs.get("first"), /beta/);
  assert.match(jobs.get("second"), /beta/);
});

test("workflow verification accepts only active exact commands", (t) => {
  const write = useFixture(t);
  const rejected = [
    "      # run: node verify.js",
    "      - name: node verify.js",
    "      - run: node verify.js || true",
    "      - run: node verify.js\n        if: false",
    "      - run: node verify.js\n        continue-on-error: true",
    "      - run: node verify.js\n        if: ${{ true }}",
    "      - run: node verify.js\n        continue-on-error: ${{ false }}",
    "      - run: |\n          cat <<EOF\n          node verify.js\n          EOF",
    "      - run: |\n          if false; then\n            node verify.js\n          fi",
    "      - run: node verify.js\n        shell: python",
    "      - run: |\n          node prepare.js\n          node verify.js",
  ];
  for (const steps of rejected) {
    write(workflow(steps));
    assert.throws(
      () => verifyWorkflows(gatePolicy),
      /missing required command/,
    );
  }
  for (const scalar of ["|", ">"]) {
    write(workflow(`      - run: ${scalar}\n          node verify.js`));
    assert.doesNotThrow(() => verifyWorkflows(gatePolicy));
  }
});

test("workflow verification rejects dynamic check identities", (t) => {
  const write = useFixture(t);
  write(
    workflow(
      "      - run: node verify.js",
      "  collision:\n    name: ${{ 'Gate' }}\n    steps:\n      - run: echo collision\n",
    ),
  );
  assert.throws(() => verifyWorkflows(gatePolicy), /dynamic check name/);
});

test("workflow verification rejects inherited shell bypasses", (t) => {
  const write = useFixture(t);
  write(`name: Fixture
defaults:
  run:
    shell: true {0}
jobs:
  gate:
    name: Gate
    steps:
      - run: node verify.js
`);
  assert.throws(() => verifyWorkflows(gatePolicy), /missing required command/);

  write(`name: Fixture
jobs:
  gate:
    name: Gate
    defaults:
      run:
        shell: true {0}
    steps:
      - run: node verify.js
`);
  assert.throws(() => verifyWorkflows(gatePolicy), /missing required command/);
});

test("workflow verification rejects conditional required jobs", (t) => {
  const write = useFixture(t);
  for (const setting of ["if: ${{ true }}", "continue-on-error: true"]) {
    write(`name: Fixture
jobs:
  gate:
    name: Gate
    ${setting}
    steps:
      - run: node verify.js
`);
    assert.throws(
      () => verifyWorkflows(gatePolicy),
      /must be unconditional and fail closed/,
    );
  }
});

test("external workflow actions require immutable pins", () => {
  const verify = (reference) =>
    verifyActionPinsInSource(`steps:\n  - uses: ${reference}\n`, "fixture.yml");
  assert.doesNotThrow(() =>
    verify("actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1"),
  );
  assert.doesNotThrow(() => verify("./.github/actions/local"));
  for (const reference of [
    "actions/checkout@v4",
    "actions/checkout@main",
    "actions/checkout@deadbeef",
  ])
    assert.throws(() => verify(reference), /full 40-character commit SHA/);
  assert.throws(
    () =>
      verifyActionPinsInSource(
        "steps:\n  - { uses: actions/checkout@v4 }\n",
        "fixture.yml",
      ),
    /full 40-character commit SHA/,
  );
  assert.throws(
    () =>
      verifyActionPinsInSource(
        "steps:\n  - uses: actions/checkout@v4 # mutable\n",
        "fixture.yml",
      ),
    /full 40-character commit SHA/,
  );
  assert.throws(() => verify("docker://alpine:latest"), /sha256 digest/);
  assert.doesNotThrow(() => verify(`docker://alpine@sha256:${"a".repeat(64)}`));
});

test("verification policy ratchet rejects removed gates and tests", () => {
  const policy = loadPolicy();
  for (const key of [
    "required_checks",
    "recovery_tests",
    "connector_conformance_tests",
    "fuzz_targets",
  ]) {
    const reduced = structuredClone(policy);
    reduced[key].pop();
    assert.throws(
      () => verifyNoRemovals(policy, reduced),
      new RegExp(`${key} removed required entry`),
    );
  }
});

test("verification policy permits only an exact declared check migration", () => {
  const shared = {
    recovery_tests: ["recovery"],
    connector_conformance_tests: ["conformance"],
    fuzz_targets: ["fuzz"],
  };
  const previous = {
    ...shared,
    required_checks: [
      {
        name: "Gate",
        workflow: "old.yml",
        job: "matrix",
        job_name: "Gate (${{ matrix.kind }})",
        commands: ["node verify.js"],
      },
    ],
  };
  const current = {
    ...shared,
    required_checks: [
      {
        name: "Gate",
        workflow: "new.yml",
        job: "static",
        job_name: "Gate",
        commands: ["node verify.js"],
      },
    ],
    required_check_migrations: [
      {
        name: "Gate",
        from: {
          workflow: "old.yml",
          job: "matrix",
          job_name: "Gate (${{ matrix.kind }})",
        },
        to: { workflow: "new.yml", job: "static", job_name: "Gate" },
        reason: "Use one stable protected check identity.",
      },
    ],
  };
  assert.doesNotThrow(() => validateRequiredCheckMigrations(current));
  assert.doesNotThrow(() => verifyNoRemovals(previous, current));
  const redirected = structuredClone(current);
  redirected.required_checks[0].job = "other";
  assert.throws(
    () => validateRequiredCheckMigrations(redirected),
    /does not match its current gate/,
  );
  assert.throws(() => verifyNoRemovals(previous, redirected), /moved Gate/);
});
