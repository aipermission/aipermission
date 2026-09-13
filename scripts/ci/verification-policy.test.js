const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");
const {
  loadPolicy,
  validateRequiredCheckMigrations,
  validateRequiredCommandMigrations,
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

test("workflow verification locks environment and working directory", (t) => {
  const write = useFixture(t);
  const contextualPolicy = structuredClone(gatePolicy);
  contextualPolicy.required_checks[0].command_working_directories = {
    "node verify.js": "scripts",
  };

  write(
    workflow(
      "      - run: node verify.js\n        working-directory: scripts",
    ),
  );
  assert.doesNotThrow(() => verifyWorkflows(contextualPolicy));

  for (const source of [
    workflow("      - run: node verify.js\n        working-directory: other"),
    `name: Fixture
defaults:
  run:
    working-directory: other
jobs:
  gate:
    name: Gate
    steps:
      - run: node verify.js
`,
    `name: Fixture
jobs:
  gate:
    name: Gate
    defaults:
      run:
        working-directory: other
    steps:
      - run: node verify.js
`,
    `name: Fixture
env:
  GOFLAGS: -run=^$
jobs:
  gate:
    name: Gate
    steps:
      - run: node verify.js
        working-directory: scripts
`,
    `name: Fixture
jobs:
  gate:
    name: Gate
    env:
      GOFLAGS: -run=^$
    steps:
      - run: node verify.js
        working-directory: scripts
`,
    workflow(
      "      - run: node verify.js\n        working-directory: scripts\n        env:\n          GOFLAGS: -run=^$",
    ),
    workflow(
      "      - run: echo 'GOFLAGS=-run=^$' >> $GITHUB_ENV\n      - run: node verify.js\n        working-directory: scripts",
    ),
  ]) {
    write(source);
    assert.throws(() => verifyWorkflows(contextualPolicy));
  }
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

test("workflow verification rejects compile-only Go test evidence", (t) => {
  const write = useFixture(t);
  write(
    workflow(
      "      - run: go test -exec=true ./...\n      - run: node verify.js",
    ),
  );
  assert.throws(() => verifyWorkflows(gatePolicy), /compile-only go test/);
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

test("verification policy ratchets command working directories", () => {
  const previous = structuredClone(gatePolicy);
  previous.recovery_tests = ["recovery"];
  previous.connector_conformance_tests = ["conformance"];
  previous.fuzz_targets = ["fuzz"];
  previous.required_checks[0].command_working_directories = {
    "node verify.js": "scripts",
  };
  const current = structuredClone(previous);
  current.required_checks[0].command_working_directories["node verify.js"] =
    "other";
  assert.throws(
    () => verifyNoRemovals(previous, current),
    /changed Gate command context/,
  );
});

test("verification policy permits only an exact command migration", () => {
  const policy = {
    required_checks: [
      {
        name: "Gate",
        workflow: "old.yml",
        job: "compile",
        job_name: "Gate",
        commands: ["node retained.js"],
      },
      {
        name: "Native Gate",
        workflow: "new.yml",
        job: "native",
        job_name: "Native Gate",
        commands: ["node native.js"],
      },
    ],
    required_command_migrations: [
      {
        name: "Replace compile-only proof",
        from: { check: "Gate", command: "node compile-only.js" },
        to: { check: "Native Gate", command: "node native.js" },
        reason: "Run the behavior on its native platform.",
      },
    ],
    recovery_tests: ["recovery"],
    connector_conformance_tests: ["conformance"],
    fuzz_targets: ["fuzz"],
  };
  const previous = structuredClone(policy);
  previous.required_checks = [
    {
      name: "Gate",
      workflow: "old.yml",
      job: "compile",
      job_name: "Gate",
      commands: ["node compile-only.js", "node retained.js"],
    },
  ];
  assert.doesNotThrow(() => validateRequiredCommandMigrations(policy));
  assert.doesNotThrow(() => verifyNoRemovals(previous, policy));

  const invalid = structuredClone(policy);
  invalid.required_command_migrations[0].to.command = "node missing.js";
  assert.throws(
    () => validateRequiredCommandMigrations(invalid),
    /does not match its current target/,
  );
});
