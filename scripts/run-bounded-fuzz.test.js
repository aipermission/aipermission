const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");

const root = path.resolve(__dirname, "..");

function walk(directory) {
  return fs.readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const entryPath = path.join(directory, entry.name);
    return entry.isDirectory() ? walk(entryPath) : [entryPath];
  });
}

test("every bounded fuzz target belongs to its configured Go package", () => {
  const source = fs.readFileSync(
    path.join(__dirname, "run-bounded-fuzz.sh"),
    "utf8",
  );
  const targets = [...source.matchAll(/^run_fuzz (\.\/\S+) (Fuzz\w+)$/gm)];
  assert.ok(targets.length > 0, "bounded fuzz runner declares no targets");
  for (const [, packagePath, target] of targets) {
    const directory = path.join(root, "backend", packagePath.slice(2));
    const declaration = new RegExp(`func\\s+${target}\\s*\\(`);
    const found = walk(directory)
      .filter((file) => file.endsWith("_test.go"))
      .some((file) => declaration.test(fs.readFileSync(file, "utf8")));
    assert.ok(found, `${target} is not declared in ${packagePath}`);
  }

  const configured = new Set(
    targets.map(([, packagePath, target]) => `${packagePath}:${target}`),
  );
  const declared = [];
  for (const file of walk(path.join(root, "backend"))) {
    if (!file.endsWith("_test.go")) continue;
    const packagePath = `./${path
      .relative(path.join(root, "backend"), path.dirname(file))
      .split(path.sep)
      .join("/")}`;
    for (const match of fs
      .readFileSync(file, "utf8")
      .matchAll(/^func\s+(Fuzz\w+)\s*\(/gm)) {
      declared.push(`${packagePath}:${match[1]}`);
    }
  }
  assert.deepEqual(
    [...configured].sort(),
    declared.sort(),
    "bounded fuzz runner must exercise every repository fuzz target",
  );
});
