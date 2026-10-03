const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const test = require("node:test");
const { execFileSync, spawnSync } = require("node:child_process");

const root = path.resolve(__dirname, "../..");

function fixture(t) {
  const directory = fs.mkdtempSync(
    path.join(os.tmpdir(), "aip-conformance-source-"),
  );
  t.after(() => fs.rmSync(directory, { recursive: true, force: true }));
  execFileSync("git", ["init", "--quiet", directory]);
  const files = {
    "scripts/conformance/run.sh": fs.readFileSync(
      path.join(root, "scripts/conformance/run.sh"),
    ),
    "scripts/verification-policy.json": "{}",
    "backend/go.mod": "module fixture\n",
    "backend/go.sum": "",
    "backend/cmd/main.go": "package main\n",
    "backend/internal/public.go": "package fixture\n",
    "backend/internal/private.go": "operator-file-not-source",
    "backend/testdata/connector-conformance/Dockerfile": "FROM fixture\n",
    "backend/testdata/connector-conformance/protocols/Dockerfile":
      "FROM fixture\n",
    "backend/testdata/connector-conformance/protocols/start.sh":
      "#!/bin/sh\nexit 0\n",
    ".gitignore": "backend/internal/private.go\n.env\n",
    ".env": "operator-file-not-source",
  };
  for (const [file, contents] of Object.entries(files)) {
    const destination = path.join(directory, file);
    fs.mkdirSync(path.dirname(destination), { recursive: true });
    fs.writeFileSync(destination, contents);
  }
  const bin = path.join(directory, "bin");
  fs.mkdirSync(bin);
  fs.writeFileSync(
    path.join(bin, "docker"),
    `#!${process.execPath}
const fs = require("node:fs");
const path = require("node:path");
const args = process.argv.slice(2);
if (args[0] === "context") {
  console.log(process.env.FIXTURE_ENDPOINT || "unix:///var/run/docker.sock");
  process.exit(0);
}
const action = args.find(value => ["build", "up", "run", "down"].includes(value));
const source = process.env.AIPERMISSION_CONFORMANCE_SOURCE;
if (action === "build") {
  for (const file of ["backend/go.mod", "backend/internal/public.go", "backend/cmd/main.go", "scripts/verification-policy.json", "backend/testdata/connector-conformance/protocols/Dockerfile", "backend/testdata/connector-conformance/protocols/start.sh"])
    if (!fs.existsSync(path.join(source, file))) process.exit(17);
  for (const file of [".env", ".git", "backend/internal/private.go"])
    if (fs.existsSync(path.join(source, file))) process.exit(18);
}
console.log(JSON.stringify({action, source, project: args[args.indexOf("-p")+1]}));
if (action === process.env.FIXTURE_FAIL ||
    (process.env.FIXTURE_FAIL === "runner" && action === "run" && args.at(-1) === "runner"))
  process.exit(9);
`,
    { mode: 0o700 },
  );
  return { directory, bin };
}

for (const failure of ["", "build", "up", "run", "runner", "down"]) {
  test(`source snapshot excludes operator files and cleans up after ${failure || "success"}`, (t) => {
    const { directory, bin } = fixture(t);
    const result = spawnSync("sh", ["scripts/conformance/run.sh"], {
      cwd: directory,
      encoding: "utf8",
      timeout: 15000,
      env: {
        ...process.env,
        PATH: `${bin}:${process.env.PATH}`,
        FIXTURE_FAIL: failure,
        DOCKER_CONTEXT: "",
        DOCKER_HOST: "",
      },
    });
    assert.equal(result.error, undefined);
    assert.equal(result.status === 0, failure === "", result.stderr);
    const events = result.stdout.trim().split("\n").map(JSON.parse);
    assert.equal(events[0].action, "build");
    const owner = events[0].project;
    assert.match(owner, /^aipermission-conformance-[a-z0-9-]+$/);
    for (const event of events) {
      assert.equal(event.project, owner);
      assert.equal(
        fs.existsSync(path.dirname(event.source)),
        false,
        "owned snapshot must be removed",
      );
    }
    assert.equal(events.at(-1).action === "down", failure !== "build");
    assert.equal(
      fs.readFileSync(path.join(directory, ".env"), "utf8"),
      "operator-file-not-source",
    );
  });
}

for (const environment of [
  { DOCKER_HOST: "tcp://remote.invalid:2375" },
  { DOCKER_CONTEXT: "remote", FIXTURE_ENDPOINT: "ssh://remote.invalid" },
  {
    DOCKER_CONTEXT: "remote",
    DOCKER_HOST: "unix:///var/run/docker.sock",
    FIXTURE_ENDPOINT: "tcp://remote.invalid:2375",
  },
]) {
  test("remote Docker endpoint is rejected before creating fixtures", (t) => {
    const { directory, bin } = fixture(t);
    const result = spawnSync("sh", ["scripts/conformance/run.sh"], {
      cwd: directory,
      encoding: "utf8",
      timeout: 15000,
      env: {
        ...process.env,
        PATH: `${bin}:${process.env.PATH}`,
        DOCKER_CONTEXT: "",
        DOCKER_HOST: "",
        ...environment,
      },
    });
    assert.equal(result.error, undefined);
    assert.equal(result.status, 1);
    assert.match(result.stderr, /local Unix-socket Docker endpoint/);
    assert.equal(result.stdout, "");
  });
}
