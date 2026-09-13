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
} = require("../../verification-policy");

const fixture = path.join(__dirname, "../../..", "fixture.yml");
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
const check = (overrides = {}) => ({
  ...gatePolicy.required_checks[0],
  ...overrides,
});
const completePolicy = (required_checks) => ({
  required_checks,
  recovery_tests: ["recovery"],
  connector_conformance_tests: ["conformance"],
  fuzz_targets: ["fuzz"],
});
const rejects = (call, pattern) => assert.throws(call, pattern);
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
    rejects(() => verifyWorkflows(gatePolicy), /missing required command/);
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
  write(
    "name: Fixture\ndefaults:\n  run:\n    shell: true {0}\njobs:\n  gate:\n    name: Gate\n    steps:\n      - run: node verify.js\n",
  );
  assert.throws(() => verifyWorkflows(gatePolicy), /missing required command/);

  write(
    "name: Fixture\njobs:\n  gate:\n    name: Gate\n    defaults:\n      run:\n        shell: true {0}\n    steps:\n      - run: node verify.js\n",
  );
  assert.throws(() => verifyWorkflows(gatePolicy), /missing required command/);
});

test("workflow verification locks environment and working directory", (t) => {
  const write = useFixture(t);
  const contextualPolicy = structuredClone(gatePolicy);
  contextualPolicy.required_checks[0].command_working_directories = {
    "node verify.js": "scripts",
  };

  write(
    workflow("      - run: node verify.js\n        working-directory: scripts"),
  );
  assert.doesNotThrow(() => verifyWorkflows(contextualPolicy));

  for (const source of [
    workflow("      - run: node verify.js\n        working-directory: other"),
    "name: Fixture\ndefaults:\n  run:\n    working-directory: other\njobs:\n  gate:\n    name: Gate\n    steps:\n      - run: node verify.js\n",
    "name: Fixture\njobs:\n  gate:\n    name: Gate\n    defaults:\n      run:\n        working-directory: other\n    steps:\n      - run: node verify.js\n",
    "name: Fixture\nenv:\n  GOFLAGS: -run=^$\njobs:\n  gate:\n    name: Gate\n    steps:\n      - run: node verify.js\n        working-directory: scripts\n",
    "name: Fixture\njobs:\n  gate:\n    name: Gate\n    env:\n      GOFLAGS: -run=^$\n    steps:\n      - run: node verify.js\n        working-directory: scripts\n",
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
    write(
      `name: Fixture\njobs:\n  gate:\n    name: Gate\n    ${setting}\n    steps:\n      - run: node verify.js\n`,
    );
    rejects(
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
    rejects(
      () => verifyNoRemovals(policy, reduced),
      new RegExp(`${key} removed required entry`),
    );
  }
});

test("verification policy permits only an exact declared check migration", () => {
  const previous = completePolicy([
    check({
      workflow: "old.yml",
      job: "matrix",
      job_name: "Gate (${{ matrix.kind }})",
    }),
  ]);
  const current = {
    ...completePolicy([
      check({ workflow: "new.yml", job: "static", job_name: "Gate" }),
    ]),
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
  previous.required_check_migrations = structuredClone(
    current.required_check_migrations,
  );
  const preauthorized = structuredClone(current);
  preauthorized.required_checks = structuredClone(previous.required_checks);
  assert.doesNotThrow(() => validateRequiredCheckMigrations(preauthorized));
  assert.doesNotThrow(() => verifyNoRemovals(previous, preauthorized));
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
  const previous = completePolicy(structuredClone(gatePolicy.required_checks));
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
  const preauthorized = structuredClone(policy);
  preauthorized.required_checks = structuredClone(previous.required_checks);
  assert.doesNotThrow(() => validateRequiredCommandMigrations(preauthorized));
  assert.doesNotThrow(() => validateRequiredCommandMigrations(policy));
  assert.doesNotThrow(() => verifyNoRemovals(previous, policy));

  const invalid = structuredClone(policy);
  invalid.required_command_migrations[0].to.command = "node missing.js";
  assert.throws(
    () => validateRequiredCommandMigrations(invalid),
    /does not match its current target/,
  );
});

test("verification policy rejects candidate-authored migrations", () => {
  const previous = completePolicy(structuredClone(gatePolicy.required_checks));

  const moved = structuredClone(previous);
  moved.required_checks[0] = {
    ...moved.required_checks[0],
    workflow: "replacement.yml",
    job: "replacement",
  };
  moved.required_check_migrations = [
    {
      name: "Gate",
      from: {
        workflow: "fixture.yml",
        job: "gate",
        job_name: "Gate",
      },
      to: {
        workflow: "replacement.yml",
        job: "replacement",
        job_name: "Gate",
      },
      reason: "Candidate-authored migration must not authorize itself.",
    },
  ];
  assert.throws(() => verifyNoRemovals(previous, moved), /moved Gate/);

  const replaced = structuredClone(previous);
  replaced.required_checks[0].commands = ["node replacement.js"];
  replaced.required_command_migrations = [
    {
      name: "Replace required verification",
      from: { check: "Gate", command: "node verify.js" },
      to: { check: "Gate", command: "node replacement.js" },
      reason: "Candidate-authored migration must not authorize itself.",
    },
  ];
  assert.throws(
    () => verifyNoRemovals(previous, replaced),
    /removed Gate command node verify\.js/,
  );
});
