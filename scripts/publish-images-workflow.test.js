#!/usr/bin/env node

const assert = require("node:assert/strict");
const childProcess = require("node:child_process");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const test = require("node:test");

const repositoryRoot = path.join(__dirname, "..");
const workflow = fs.readFileSync(path.join(repositoryRoot, ".github", "workflows", "publish-images.yml"), "utf8");
const promotionScript = path.join(__dirname, "promote-container-images.sh");
const candidateScript = path.join(__dirname, "build-container-candidate.sh");

function writeExecutable(filePath, contents) {
  fs.writeFileSync(filePath, contents, { mode: 0o755 });
}

function createFakeDocker(directory, state) {
  const binDirectory = path.join(directory, "bin");
  const statePath = path.join(directory, "state.json");
  const logPath = path.join(directory, "docker.log");
  fs.mkdirSync(binDirectory);
  fs.writeFileSync(statePath, JSON.stringify(state));
  writeExecutable(
    path.join(binDirectory, "docker"),
    `#!/usr/bin/env node
const fs = require("node:fs");
const args = process.argv.slice(2);
fs.appendFileSync(process.env.FAKE_DOCKER_LOG, args.join(" ") + "\\n");
const state = JSON.parse(fs.readFileSync(process.env.FAKE_DOCKER_STATE, "utf8"));
if (args[0] !== "buildx") process.exit(2);
if (args[1] === "imagetools" && args[2] === "inspect") {
  const digest = state[args[3]];
  if (!digest && args[3] === process.env.FAKE_DOCKER_MUTATE_ON_MISSING_INSPECT_TARGET) {
    state[args[3]] = process.env.FAKE_DOCKER_MUTATION_DIGEST;
    fs.writeFileSync(process.env.FAKE_DOCKER_STATE, JSON.stringify(state));
  }
  if (!digest) { console.error("manifest unknown"); process.exit(1); }
  console.log("Digest: " + digest);
  process.exit(0);
}
if (args[1] === "imagetools" && args[2] === "create") {
  const target = args[args.indexOf("--tag") + 1];
  if (target === process.env.FAKE_DOCKER_FAIL_CREATE_TARGET) process.exit(42);
  const source = args.at(-1);
  state[target] = source.slice(source.lastIndexOf("@") + 1);
  fs.writeFileSync(process.env.FAKE_DOCKER_STATE, JSON.stringify(state));
  process.exit(0);
}
if (args[1] === "build") {
  const metadata = args[args.indexOf("--metadata-file") + 1];
  fs.writeFileSync(metadata, JSON.stringify({ "containerimage.digest": process.env.FAKE_BUILD_DIGEST }));
  process.exit(0);
}
process.exit(2);
`,
  );
  return { binDirectory, statePath, logPath };
}

function runExistingCandidate({ cosignMode = "success", predicateOverrides = {} } = {}) {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "aipermission-image-candidate-"));
  const release = "ghcr.io/aipermission/aipermission-backend:v0.2.38";
  const fake = createFakeDocker(directory, { [release]: "sha256:verified" });
  const outputPath = path.join(directory, "github-output");
  const cosignLog = path.join(directory, "cosign.log");
  const predicate = Buffer.from(
    JSON.stringify({
      predicate: {
        repository: "aipermission/aipermission",
        commit: "commit-sha",
        workflow: ".github/workflows/publish-images.yml",
        ref: "refs/tags/v0.2.38",
        ...predicateOverrides,
      },
    }),
  ).toString("base64");
  writeExecutable(
    path.join(fake.binDirectory, "cosign"),
    `#!/usr/bin/env node
const fs = require("node:fs");
const args = process.argv.slice(2);
fs.appendFileSync(process.env.FAKE_COSIGN_LOG, args.join(" ") + "\\n");
if (process.env.FAKE_COSIGN_MODE === "signature-failure" && args[0] === "verify") process.exit(1);
if (process.env.FAKE_COSIGN_MODE === "attestation-failure" && args[0] === "verify-attestation") process.exit(1);
if (args[0] === "verify-attestation" && process.env.FAKE_COSIGN_MODE !== "missing-attestation") {
  console.log(JSON.stringify({ payload: process.env.FAKE_ATTESTATION_PAYLOAD }));
}
`,
  );

  const result = childProcess.spawnSync("bash", [candidateScript, "backend", "./backend"], {
    cwd: repositoryRoot,
    encoding: "utf8",
    env: {
      ...process.env,
      PATH: `${fake.binDirectory}:${process.env.PATH}`,
      FAKE_DOCKER_LOG: fake.logPath,
      FAKE_DOCKER_STATE: fake.statePath,
      FAKE_BUILD_DIGEST: "sha256:different-rebuild",
      FAKE_COSIGN_LOG: cosignLog,
      FAKE_COSIGN_MODE: cosignMode,
      FAKE_ATTESTATION_PAYLOAD: predicate,
      GITHUB_OUTPUT: outputPath,
      GITHUB_REF_NAME: "v0.2.38",
      GITHUB_REF: "refs/tags/v0.2.38",
      GITHUB_SHA: "commit-sha",
      GITHUB_REPOSITORY: "aipermission/aipermission",
      RUNNER_TEMP: directory,
    },
  });
  return {
    result,
    output: fs.existsSync(outputPath) ? fs.readFileSync(outputPath, "utf8") : "",
    dockerLog: fs.readFileSync(fake.logPath, "utf8"),
    cosignLog: fs.readFileSync(cosignLog, "utf8"),
  };
}

function runPromotion(state, overrides = {}) {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "aipermission-image-promotion-"));
  const fake = createFakeDocker(directory, state);
  const result = childProcess.spawnSync("bash", [promotionScript], {
    encoding: "utf8",
    env: {
      ...process.env,
      PATH: `${fake.binDirectory}:${process.env.PATH}`,
      FAKE_DOCKER_LOG: fake.logPath,
      FAKE_DOCKER_STATE: fake.statePath,
      GITHUB_REF_NAME: "v0.2.38",
      BACKEND_DIGEST: "sha256:new-backend",
      FRONTEND_DIGEST: "sha256:new-frontend",
      ...overrides,
    },
  });
  return {
    result,
    state: JSON.parse(fs.readFileSync(fake.statePath, "utf8")),
    log: fs.existsSync(fake.logPath) ? fs.readFileSync(fake.logPath, "utf8") : "",
  };
}

test("container release validates both candidates before promotion", () => {
  const build = workflow.indexOf("Build both candidate images");
  const scan = workflow.indexOf("Scan both exact candidate digests");
  const sign = workflow.indexOf("Sign and attest both candidate digests");
  const promote = workflow.indexOf("Promote signed pair with rollback");
  assert.ok(build >= 0 && build < scan && scan < sign && sign < promote);
  assert.doesNotMatch(workflow, /strategy:\s*\n\s+matrix:/);
});

test("release reruns reuse only signed immutable images with matching source attestations", () => {
  const script = fs.readFileSync(candidateScript, "utf8");
  assert.match(script, /resolve_optional_digest "\$\{release\}"/);
  assert.doesNotMatch(script, /resolve_optional_digest "\$\{candidate\}"/);
  assert.match(script, /cosign verify \\/);
  assert.match(script, /cosign verify-attestation \\/);
  assert.match(script, /\$p\.repository == \$repository/);
  assert.match(script, /\$p\.commit == \$commit/);
});

test("verified immutable release digest wins over a hypothetical different rebuild", () => {
  const { result, output, dockerLog, cosignLog } = runExistingCandidate();
  assert.equal(result.status, 0, result.stdout + result.stderr);
  assert.match(output, /backend_digest=sha256:verified/);
  assert.doesNotMatch(dockerLog, /buildx build/);
  assert.match(cosignLog, /^verify /m);
  assert.match(cosignLog, /^verify-attestation /m);
});

test("immutable reuse rejects failed verification and every mismatched source claim", () => {
  const cases = [
    ["failed signature", { cosignMode: "signature-failure" }],
    ["failed attestation verification", { cosignMode: "attestation-failure" }],
    ["missing attestation", { cosignMode: "missing-attestation" }],
    ["repository mismatch", { predicateOverrides: { repository: "other/repository" } }],
    ["commit mismatch", { predicateOverrides: { commit: "other-commit" } }],
    ["workflow mismatch", { predicateOverrides: { workflow: ".github/workflows/other.yml" } }],
    ["ref mismatch", { predicateOverrides: { ref: "refs/tags/v9.9.9" } }],
  ];
  for (const [description, options] of cases) {
    const { result, output, dockerLog } = runExistingCandidate(options);
    assert.notEqual(result.status, 0, `${description} should be rejected`);
    assert.equal(output, "", `${description} wrote candidate outputs`);
    assert.doesNotMatch(dockerLog, /buildx build/, `${description} triggered a rebuild fallback`);
  }
});

test("immutable tag conflicts fail before any registry mutation", () => {
  const { result, log } = runPromotion({
    "ghcr.io/aipermission/aipermission-backend:latest": "sha256:old-backend",
    "ghcr.io/aipermission/aipermission-frontend:latest": "sha256:old-frontend",
    "ghcr.io/aipermission/aipermission-backend:v0.2.38": "sha256:conflict",
  });

  assert.notEqual(result.status, 0, result.stdout + result.stderr);
  assert.match(result.stderr, /Refusing to replace existing immutable image tag/);
  assert.doesNotMatch(log, /imagetools create/);
});

test("frontend immutable conflicts are also found before backend mutation", () => {
  const { result, log } = runPromotion({
    "ghcr.io/aipermission/aipermission-backend:latest": "sha256:old-backend",
    "ghcr.io/aipermission/aipermission-frontend:latest": "sha256:old-frontend",
    "ghcr.io/aipermission/aipermission-frontend:0.2.38": "sha256:conflict",
  });

  assert.notEqual(result.status, 0, result.stdout + result.stderr);
  assert.match(result.stderr, /Refusing to replace existing immutable image tag/);
  assert.doesNotMatch(log, /imagetools create/);
});

test("an immutable tag inserted after preflight is rejected and remains untouched", () => {
  const target = "ghcr.io/aipermission/aipermission-frontend:0.2.38";
  const { result, state, log } = runPromotion(
    {
      "ghcr.io/aipermission/aipermission-backend:latest": "sha256:old-backend",
      "ghcr.io/aipermission/aipermission-frontend:latest": "sha256:old-frontend",
    },
    {
      FAKE_DOCKER_MUTATE_ON_MISSING_INSPECT_TARGET: target,
      FAKE_DOCKER_MUTATION_DIGEST: "sha256:concurrent-writer",
    },
  );

  assert.notEqual(result.status, 0, result.stdout + result.stderr);
  assert.match(result.stderr, /changed after preflight/);
  assert.equal(state[target], "sha256:concurrent-writer");
  assert.doesNotMatch(log, new RegExp(`--tag ${target.replaceAll(".", "\\.")}`));
});

test("partial latest promotion restores both previous digests", () => {
  const { result, state } = runPromotion(
    {
      "ghcr.io/aipermission/aipermission-backend:latest": "sha256:old-backend",
      "ghcr.io/aipermission/aipermission-frontend:latest": "sha256:old-frontend",
    },
    { FAKE_DOCKER_FAIL_CREATE_TARGET: "ghcr.io/aipermission/aipermission-frontend:latest" },
  );

  assert.notEqual(result.status, 0, result.stdout + result.stderr);
  assert.equal(state["ghcr.io/aipermission/aipermission-backend:latest"], "sha256:old-backend");
  assert.equal(state["ghcr.io/aipermission/aipermission-frontend:latest"], "sha256:old-frontend");
});

test("first release preserves immutable version tags without an unsafe latest mutation", () => {
  const { result, state, log } = runPromotion({});

  assert.equal(result.status, 0, result.stdout + result.stderr);
  assert.equal(state["ghcr.io/aipermission/aipermission-backend:v0.2.38"], "sha256:new-backend");
  assert.equal(state["ghcr.io/aipermission/aipermission-backend:0.2.38"], "sha256:new-backend");
  assert.equal(state["ghcr.io/aipermission/aipermission-frontend:v0.2.38"], "sha256:new-frontend");
  assert.equal(state["ghcr.io/aipermission/aipermission-frontend:0.2.38"], "sha256:new-frontend");
  assert.equal(state["ghcr.io/aipermission/aipermission-backend:latest"], undefined);
  assert.equal(state["ghcr.io/aipermission/aipermission-frontend:latest"], undefined);
  assert.doesNotMatch(log, /--tag ghcr\.io\/aipermission\/aipermission-(?:backend|frontend):latest/);
});

test("asymmetric previous latest state fails before any registry mutation", () => {
  const { result, log } = runPromotion({
    "ghcr.io/aipermission/aipermission-backend:latest": "sha256:old-backend",
  });

  assert.notEqual(result.status, 0, result.stdout + result.stderr);
  assert.match(result.stderr, /only one image has a restorable previous latest digest/);
  assert.doesNotMatch(log, /imagetools create/);
});

test("container release is resumable without deleting package versions", () => {
  const script = fs.readFileSync(promotionScript, "utf8");
  assert.match(script, /ensure_immutable_tag "\$\{backend\}:\$\{GITHUB_REF_NAME\}"/);
  assert.match(script, /ensure_immutable_tag "\$\{frontend\}:\$\{version\}"/);
  assert.match(script, /already points to the expected digest/);
  assert.match(script, /run_promotion/);
  assert.match(script, /rollback/);
  assert.match(script, /trap 'interrupt_with_rollback 130' INT/);
  assert.match(script, /trap 'interrupt_with_rollback 143' TERM/);
  assert.doesNotMatch(workflow, /gh api --method DELETE|delete_candidate_version/);
});
