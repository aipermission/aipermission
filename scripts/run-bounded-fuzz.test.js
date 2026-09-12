const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");
const os = require("node:os");
const { spawnSync } = require("node:child_process");

const root = path.resolve(__dirname, "..");

test("bounded fuzz runner validates the complete Go-syntax inventory", () => {
  const source = fs.readFileSync(
    path.join(__dirname, "run-bounded-fuzz.sh"),
    "utf8",
  );
  assert.match(source, /verification-policy\.js" --list fuzz_targets/);
  assert.match(source, /go run \.\/cmd\/verification-runner fuzz-inventory/);
  const policy = JSON.parse(
    fs.readFileSync(path.join(__dirname, "verification-policy.json"), "utf8"),
  );
  const targets = policy.fuzz_targets.map(({ package: packagePath, name }) => `${packagePath}:${name}`);
  assert.ok(targets.length > 0, "bounded fuzz runner declares no targets");
  assert.equal(new Set(targets).size, targets.length);
  assert.ok(policy.fuzz_targets.every(({ package: packagePath, name }) => packagePath.startsWith("./") && /^Fuzz\w+$/.test(name)));
});

test("bounded fuzz runner fails when inventory production fails", () => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "aipermission-fuzz-"));
  const fakeNode = path.join(directory, "node");
  try {
    fs.writeFileSync(fakeNode, "#!/bin/sh\nexit 17\n", { mode: 0o700 });
    const result = spawnSync(
      "sh",
      [path.join(__dirname, "run-bounded-fuzz.sh")],
      {
        cwd: root,
        encoding: "utf8",
        env: { ...process.env, PATH: `${directory}:${process.env.PATH}` },
      },
    );
    assert.notEqual(result.status, 0);
    assert.match(result.stderr, /failed to produce the bounded fuzz target inventory/);
  } finally {
    fs.rmSync(directory, { recursive: true, force: true });
  }
});
