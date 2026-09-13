const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const test = require("node:test");

const {
  commitFileUpdates,
  pinnedDockerReleaseValue,
  requireMCPConfigPlaceholder,
  stagePinnedDockerCompose,
} = require("../release-version");

test("release staging keeps MCP placeholders and pins every Docker image", () => {
  const image = (name) =>
    `image: ghcr.io/aipermission/${name}:\${AIPERMISSION_VERSION:-1.0.0}`;
  const updates = new Map([
    ["README.md", "npx -y @aipermission/mcp@VERSION setup\n"],
    [
      "docker-compose.release.yml",
      [
        image("aipermission-backend"),
        image("aipermission-backend"),
        image("aipermission-frontend"),
        image("aipermission-frontend"),
      ].join("\n"),
    ],
  ]);
  stagePinnedDockerCompose(updates, "1.2.3");
  assert.match(updates.get("README.md"), /@aipermission\/mcp@VERSION/);
  assert.equal(
    pinnedDockerReleaseValue(
      updates.get("docker-compose.release.yml"),
      "1.2.3",
    ),
    "1.2.3",
  );
});

test("MCP config docs require placeholders for every setup example", () => {
  const validate = (source) => requireMCPConfigPlaceholder(source, "README.md");
  assert.doesNotThrow(() =>
    validate("@aipermission/mcp@VERSION\n@aipermission/mcp@VERSION"),
  );
  for (const source of [
    "@aipermission/mcp@0.2.40",
    "@aipermission/mcp@VERSION\n@aipermission/mcp@0.2.40",
  ]) {
    assert.throws(() => validate(source), /VERSION placeholder/);
  }
});

test("pinned Docker release detection rejects missing and stale image sets", () => {
  const version = "1.2.3";
  const image = (name, value) =>
    `image: aipermission-${name}:\${AIPERMISSION_VERSION:-${value}}`;
  assert.equal(
    pinnedDockerReleaseValue(
      [
        image("backend", version),
        image("backend", version),
        image("frontend", version),
        image("frontend", version),
      ].join("\n"),
      version,
    ),
    version,
  );
  assert.equal(
    pinnedDockerReleaseValue(image("backend", version), version),
    undefined,
  );
  assert.equal(
    pinnedDockerReleaseValue(
      [
        image("backend", "1.2.2"),
        image("backend", "1.2.2"),
        image("frontend", "1.2.1"),
        image("frontend", "1.2.1"),
      ].join("\n"),
      version,
    ),
    "1.2.2,1.2.2,1.2.1,1.2.1",
  );
});

test("file update restores earlier files when a later rename fails", (t) => {
  const { directory, first, second } = twoFileFixture(t, "rename");
  let failed = false;
  assert.throws(
    () =>
      commitFileUpdates(directory, updates(), {
        renameSync(source, target) {
          if (target === second && !failed) {
            failed = true;
            throw new Error("simulated second rename failure");
          }
          fs.renameSync(source, target);
        },
      }),
    /simulated second rename failure/,
  );
  assert.equal(fs.readFileSync(first, "utf8"), "before-first");
  assert.equal(fs.readFileSync(second, "utf8"), "before-second");
  assert.deepEqual(fs.readdirSync(directory).sort(), [
    "first.txt",
    "second.txt",
  ]);
});

test("file update removes a partially written preparation file", (t) => {
  const directory = tempDirectory(t, "partial");
  const target = path.join(directory, "release.txt");
  fs.writeFileSync(target, "before");
  assert.throws(
    () =>
      commitFileUpdates(directory, new Map([["release.txt", "after"]]), {
        writeFileSync(filePath, contents, options) {
          fs.writeFileSync(filePath, String(contents).slice(0, 2), options);
          throw new Error("simulated partial write");
        },
      }),
    /simulated partial write/,
  );
  assert.equal(fs.readFileSync(target, "utf8"), "before");
  assert.deepEqual(fs.readdirSync(directory), ["release.txt"]);
});

test("file update removes a partially written rollback file", (t) => {
  const { directory } = twoFileFixture(t, "rollback");
  let writes = 0;
  let renames = 0;
  assert.throws(
    () =>
      commitFileUpdates(directory, updates(), {
        writeFileSync(filePath, contents, options) {
          fs.writeFileSync(filePath, contents, options);
          if (++writes === 3)
            throw new Error("simulated rollback write failure");
        },
        renameSync(source, target) {
          if (++renames === 2)
            throw new Error("simulated commit rename failure");
          fs.renameSync(source, target);
        },
      }),
    /rollback also failed/,
  );
  assert.deepEqual(fs.readdirSync(directory).sort(), [
    "first.txt",
    "second.txt",
  ]);
});

function tempDirectory(t, name) {
  const directory = fs.mkdtempSync(
    path.join(os.tmpdir(), `aipermission-release-${name}-`),
  );
  t.after(() => fs.rmSync(directory, { recursive: true, force: true }));
  return directory;
}

function twoFileFixture(t, name) {
  const directory = tempDirectory(t, name);
  const first = path.join(directory, "first.txt");
  const second = path.join(directory, "second.txt");
  fs.writeFileSync(first, "before-first");
  fs.writeFileSync(second, "before-second");
  return { directory, first, second };
}

function updates() {
  return new Map([
    ["first.txt", "after-first"],
    ["second.txt", "after-second"],
  ]);
}
