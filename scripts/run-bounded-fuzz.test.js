const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");
const os = require("node:os");
const { spawnSync } = require("node:child_process");
const { verifyFuzzEvents } = require("./verify-fuzz-events");

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

function event(action, target, output = "") {
  return JSON.stringify({
    Action: action,
    Package: "example/fuzz",
    ...(target ? { Test: target } : {}),
    ...(output ? { Output: output } : {}),
  });
}

test("fuzz evidence requires generated input execution and terminal passes", () => {
  const target = "FuzzBoundary";
  const valid = [
    event("start"),
    event("run", target),
    event("output", target, "fuzz: elapsed: 0s, gathering baseline coverage: 3/3 completed, now fuzzing with 1 workers\n"),
    event("output", target, "fuzz: elapsed: 0s, execs: 100 (100/sec), new interesting: 0 (total: 3)\n"),
    event("pass", target),
    event("pass"),
  ].join("\n");
  assert.deepEqual(verifyFuzzEvents(valid, target, "100x"), { executions: 100 });
  assert.throws(
    () => verifyFuzzEvents(valid, target, "101x"),
    /reported 100 executions for 101x budget/,
  );

  const mutations = [
    valid.replace(event("run", target), ""),
    valid.replace(event("pass", target), event("skip", target)),
    valid.replace(event("pass", target), event("fail", target)),
    valid.replace(/gathering baseline coverage:[^\\"]+now fuzzing with 1 workers\\n/, "gathering baseline coverage: 0/3 completed\\n"),
    valid.replace(/execs: 100/, "execs: 0"),
    valid.replace(event("pass", target), ""),
    valid.slice(0, valid.lastIndexOf(event("pass"))),
    valid.replace(event("pass"), JSON.stringify({ Action: "pass", Package: "example/other" })),
    valid.replace(event("pass", target), "") + `\n${event("pass", target)}`,
  ];
  for (const source of mutations) {
    assert.throws(() => verifyFuzzEvents(source, target), /execution evidence rejected/);
  }
});

test("fuzz evidence rejects malformed event streams", () => {
  assert.throws(
    () => verifyFuzzEvents('{"Action":"run"}\nnot-json', "FuzzBoundary"),
    /invalid go test JSON event/,
  );
});
