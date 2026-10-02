const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const { spawnSync } = require("node:child_process");
const path = require("node:path");
const test = require("node:test");
const {
  loadPolicy,
  localReleaseRecipeHashes,
  validateLocalReleaseRecipeMigrations,
  validateRequiredCheckMigrations,
  validateRequiredCommandMigrations,
  verifyActionPinsInSource,
  verifyNoRemovals,
  verifyLocalReleaseTargets,
  verifyRepositoryWorkflows,
  verifyWorkflows,
  workflowJobs,
} = require("../verification-policy");

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
  const root = fs.mkdtempSync(
    path.join(os.tmpdir(), "aipermission-workflow-fixture-"),
  );
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  return Object.assign(
    (source) => fs.writeFileSync(path.join(root, "fixture.yml"), source),
    {
      root,
      actionPath: path.join(root, ".github", "actions", "verification-fixture"),
      verify: (policy = gatePolicy) => verifyWorkflows(policy, root),
    },
  );
}

function writeLocalActionFixture(write, source) {
  fs.mkdirSync(write.actionPath, { recursive: true });
  fs.writeFileSync(path.join(write.actionPath, "action.yml"), source);
}
const localActionWorkflow = workflow(
  "      - uses: ./.github/actions/verification-fixture\n      - run: node verify.js",
);
const safeLocalAction =
  "name: Safe fixture\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo safe\n";
const check = (overrides = {}) => ({
  ...structuredClone(gatePolicy.required_checks[0]),
  ...overrides,
});
const completePolicy = (required_checks = [check()]) => ({
  required_checks,
  local_release_targets: ["hygiene"],
  local_release_recipe_hashes: {},
  recovery_tests: ["recovery"],
  connector_conformance_tests: ["conformance"],
  fuzz_targets: ["fuzz"],
});
const rejects = (call, pattern) => assert.throws(call, pattern);
test("local release checks exactly match the verification policy", () => {
  assert.doesNotThrow(() => verifyLocalReleaseTargets());
  const valid =
    "hygiene:\n\ttrue\nbackend-test:\n\ttrue\nrelease-check: hygiene backend-test\n";
  const targets = ["hygiene", "backend-test"];
  const policy = {
    local_release_targets: targets,
    local_release_recipe_hashes: localReleaseRecipeHashes(valid, targets),
  };
  const verify = (source) => verifyLocalReleaseTargets(policy, source);
  assert.doesNotThrow(() => verify(valid));
  for (const [source, pattern] of [
    [valid.replace(" hygiene", ""), /differ from verification policy/],
    [
      valid.replace("backend-test:\n\ttrue", "backend-test:\n\tfalse"),
      /target recipes differ from verification policy/,
    ],
    [`${valid}\nhygiene:\n\tfalse\n`, /duplicate hygiene target definitions/],
    [
      `${valid}\nhygiene bypass:\n\tfalse\n`,
      /duplicate hygiene target definitions/,
    ],
    [`${valid}\nhygiene&:\n\tfalse\n`, /duplicate hygiene target definitions/],
    [
      `${valid}\nbypass \\\n+  hygiene:\n\tfalse\n`,
      /duplicate hygiene target definitions/,
    ],
    [
      `${valid}\nrelease-check: hygiene backend-test\n`,
      /duplicate release-check target definitions/,
    ],
    [
      `SHELL := /bin/true\n${valid}`,
      /target recipes differ from verification policy/,
    ],
    [
      `include override.mk\n${valid}`,
      /must not include mutable external makefiles/,
    ],
    [
      `$(eval include override.mk)\n${valid}`,
      /must not generate rules with eval/,
    ],
  ]) {
    assert.throws(() => verify(source), pattern);
  }
});
test("workflow parser scopes commands to their owning job", () => {
  const jobs = workflowJobs(
    "name: CI\njobs:\n  first:\n    steps:\n      - run: alpha\n  second:\n    steps:\n      - run: beta\n",
  );
  assert.match(jobs.get("first"), /alpha/);
  assert.doesNotMatch(jobs.get("first"), /beta/);
  assert.match(jobs.get("second"), /beta/);
});
test("repository workflows bound runtime and cancel only stale PR runs", () => {
  assert.doesNotThrow(() => verifyRepositoryWorkflows());
});
test("an invalid held fixture cannot affect another fixture or a separate repository verifier", (t) => {
  const invalid = useFixture(t);
  const valid = useFixture(t);
  invalid(workflow("      - run: node verify.js"));
  valid(workflow("      - run: node verify.js"));
  fs.writeFileSync(
    path.join(invalid.root, "GNUmakefile"),
    "release-check:\n\tfalse\n",
  );
  assert.notEqual(invalid.root, valid.root);
  assert.throws(() => invalid.verify(), /alternate root make entrypoint/);
  assert.doesNotThrow(() => valid.verify());
  const result = spawnSync(
    process.execPath,
    [
      path.resolve(__dirname, "../verification-policy.js"),
      "--verify-workflows",
    ],
    {
      timeout: 10000,
      encoding: "utf8",
    },
  );
  assert.equal(result.status, 0, result.stderr);
  assert.equal(fs.existsSync(path.join(invalid.root, "GNUmakefile")), true);
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
    rejects(() => write.verify(), /missing required command/);
  }
  for (const scalar of ["|", ">"]) {
    write(workflow(`      - run: ${scalar}\n          node verify.js`));
    assert.doesNotThrow(() => write.verify());
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
  assert.throws(() => write.verify(), /dynamic check name/);
});

test("workflow verification rejects inherited shell bypasses", (t) => {
  const write = useFixture(t);
  write(
    "name: Fixture\ndefaults:\n  run:\n    shell: true {0}\njobs:\n  gate:\n    name: Gate\n    steps:\n      - run: node verify.js\n",
  );
  assert.throws(() => write.verify(), /missing required command/);

  write(
    "name: Fixture\njobs:\n  gate:\n    name: Gate\n    defaults:\n      run:\n        shell: true {0}\n    steps:\n      - run: node verify.js\n",
  );
  assert.throws(() => write.verify(), /missing required command/);
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
  assert.doesNotThrow(() => write.verify(contextualPolicy));

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
    assert.throws(() => write.verify(contextualPolicy));
  }
});

test("workflow verification rejects conditional required jobs", (t) => {
  const write = useFixture(t);
  for (const setting of ["if: ${{ true }}", "continue-on-error: true"]) {
    write(
      `name: Fixture\njobs:\n  gate:\n    name: Gate\n    ${setting}\n    steps:\n      - run: node verify.js\n`,
    );
    rejects(() => write.verify(), /must be unconditional and fail closed/);
  }
});

test("workflow verification rejects compile-only Go test evidence", (t) => {
  const write = useFixture(t);
  write(
    workflow(
      "      - run: go test -exec=true ./...\n      - run: node verify.js",
    ),
  );
  assert.throws(() => write.verify(), /compile-only go test/);
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
  for (const source of [
    "steps:\n  - { uses: actions/checkout@v4 }\n",
    "steps:\n  - uses: actions/checkout@v4 # mutable\n",
  ]) {
    assert.throws(
      () => verifyActionPinsInSource(source, "fixture.yml"),
      /full 40-character commit SHA/,
    );
  }
  assert.throws(() => verify("docker://alpine:latest"), /sha256 digest/);
  assert.doesNotThrow(() => verify(`docker://alpine@sha256:${"a".repeat(64)}`));
});

test("required jobs recursively reject unsafe local composite actions", (t) => {
  const write = useFixture(t);
  write(localActionWorkflow);
  writeLocalActionFixture(write, safeLocalAction);
  assert.doesNotThrow(() => write.verify());

  for (const unsafeStep of [
    "    - shell: bash\n      run: echo 'GOFLAGS=-run=^$' >> $GITHUB_ENV\n",
    "    - shell: bash\n      run: echo /tmp/fake-bin >> $GITHUB_PATH\n",
    "    - shell: bash\n      env:\n        GOFLAGS: -run=^$\n      run: echo unsafe\n",
    "    - shell: bash\n      if: ${{ true }}\n      run: echo unsafe\n",
    "    - shell: python\n      run: print('unsafe')\n",
  ]) {
    fs.writeFileSync(
      path.join(write.actionPath, "action.yml"),
      `name: Unsafe fixture\nruns:\n  using: composite\n  steps:\n${unsafeStep}`,
    );
    assert.throws(
      () => write.verify(),
      /environment|persistent|bash|unconditional/,
    );
  }
});

test("required jobs resolve aliased roots without accepting local action escapes", (t) => {
  const write = useFixture(t);
  const outside = useFixture(t);
  const alias = path.join(outside.root, "repository-alias");
  const verifyAlias = () => verifyWorkflows(gatePolicy, alias);
  fs.symlinkSync(write.root, alias, "dir");
  writeLocalActionFixture(write, safeLocalAction);
  write(localActionWorkflow);
  assert.doesNotThrow(verifyAlias);
  write(workflow("      - uses: ./../outside\n      - run: node verify.js"));
  assert.throws(verifyAlias, /escapes the repository/);
  fs.symlinkSync(outside.root, path.join(write.root, "outside-link"), "dir");
  fs.writeFileSync(
    path.join(outside.root, "action.yml"),
    "name: Outside action\n",
  );
  write(workflow("      - uses: ./outside-link\n      - run: node verify.js"));
  assert.throws(verifyAlias, /escapes the repository/);
});

test("required jobs reject local action escapes and cycles", (t) => {
  const write = useFixture(t);
  write(workflow("      - uses: ./../outside\n      - run: node verify.js"));
  assert.throws(() => write.verify(), /escapes the repository/);

  writeLocalActionFixture(
    write,
    "name: Cyclic fixture\nruns:\n  using: composite\n  steps:\n    - uses: ./.github/actions/verification-fixture\n",
  );
  write(localActionWorkflow);
  assert.throws(() => write.verify(), /local action cycle/);
});

test("workflow verification rejects alternate root make entrypoints", (t) => {
  const write = useFixture(t);
  write(workflow("      - run: node verify.js"));
  for (const filename of ["GNUmakefile", "makefile"]) {
    const alternate = path.join(write.root, filename);
    fs.writeFileSync(alternate, "release-check:\n\ttrue\n");
    assert.throws(
      () => write.verify(),
      new RegExp(`alternate root make entrypoint ${filename}`),
    );
    fs.rmSync(alternate);
  }
});

test("verification policy ratchet rejects removed gates and tests", () => {
  const policy = loadPolicy();
  for (const key of [
    "required_checks",
    "local_release_targets",
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
  const from = {
    workflow: "old.yml",
    job: "matrix",
    job_name: "Gate (${{ matrix.kind }})",
  };
  const to = { workflow: "new.yml", job: "static", job_name: "Gate" };
  const previous = completePolicy([check(from)]);
  const current = {
    ...completePolicy([check(to)]),
    required_check_migrations: [
      {
        name: "Gate",
        from,
        to,
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
  const previous = completePolicy();
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
      check({
        workflow: "old.yml",
        job: "compile",
        commands: ["node retained.js"],
      }),
      check({
        name: "Native Gate",
        workflow: "new.yml",
        job: "native",
        job_name: "Native Gate",
        commands: ["node native.js"],
      }),
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
    check({
      ...policy.required_checks[0],
      commands: ["node compile-only.js", "node retained.js"],
      command_working_directories: { "node compile-only.js": "legacy" },
    }),
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

test("verification policy ratchets local release target recipes", () => {
  const previous = completePolicy();
  previous.local_release_recipe_hashes = { hygiene: "a".repeat(64) };
  const current = structuredClone(previous);
  current.local_release_recipe_hashes.hygiene = "b".repeat(64);
  assert.throws(
    () => verifyNoRemovals(previous, current),
    /local_release_recipe_hashes changed required entry hygiene/,
  );
});

test("verification policy permits only a preauthorized recipe migration", () => {
  const from = "a".repeat(64);
  const to = "b".repeat(64);
  const migration = {
    target: "hygiene",
    from,
    to,
    reason:
      "Replace one local release recipe with reviewed equivalent evidence.",
  };
  const previous = completePolicy();
  previous.local_release_recipe_hashes = { hygiene: from };
  previous.local_release_recipe_migrations = [migration];
  const current = structuredClone(previous);
  current.local_release_recipe_hashes.hygiene = to;

  assert.doesNotThrow(() => validateLocalReleaseRecipeMigrations(previous));
  assert.doesNotThrow(() => validateLocalReleaseRecipeMigrations(current));
  assert.doesNotThrow(() => verifyNoRemovals(previous, current));

  const selfAuthorized = structuredClone(current);
  const untrustedBase = structuredClone(previous);
  untrustedBase.local_release_recipe_migrations = [];
  assert.throws(
    () => verifyNoRemovals(untrustedBase, selfAuthorized),
    /local_release_recipe_hashes changed required entry hygiene/,
  );

  const redirected = structuredClone(current);
  redirected.local_release_recipe_migrations[0].to = "c".repeat(64);
  assert.throws(
    () => validateLocalReleaseRecipeMigrations(redirected),
    /does not match its current hash/,
  );
});

test("verification policy rejects candidate-authored migrations", () => {
  const previous = completePolicy();
  const from = { workflow: "fixture.yml", job: "gate", job_name: "Gate" };
  const to = {
    workflow: "replacement.yml",
    job: "replacement",
    job_name: "Gate",
  };
  const moved = structuredClone(previous);
  moved.required_checks[0] = check(to);
  moved.required_check_migrations = [
    {
      name: "Gate",
      from,
      to,
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
