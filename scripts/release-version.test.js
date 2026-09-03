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
} = require("./release-version");

test("release staging keeps MCP docs placeholder and updates Docker image pins", () => {
  const updates = new Map([
    ["README.md", "npx -y @aipermission/mcp@VERSION setup\n"],
    [
      "docker-compose.release.yml",
      [
        "image: ghcr.io/aipermission/aipermission-backend:${AIPERMISSION_VERSION:-1.0.0}",
        "image: ghcr.io/aipermission/aipermission-backend:${AIPERMISSION_VERSION:-1.0.0}",
        "image: ghcr.io/aipermission/aipermission-frontend:${AIPERMISSION_VERSION:-1.0.0}",
      ].join("\n"),
    ],
  ]);
  stagePinnedDockerCompose(updates, "1.2.3");
  assert.match(updates.get("README.md"), /@aipermission\/mcp@VERSION/);
  assert.equal(pinnedDockerReleaseValue(updates.get("docker-compose.release.yml"), "1.2.3"), "1.2.3");
});

test("MCP config docs require placeholders for every setup example", () => {
  assert.doesNotThrow(() =>
    requireMCPConfigPlaceholder(
      "@aipermission/mcp@VERSION\n@aipermission/mcp@VERSION",
      "README.md",
    ),
  );
  assert.throws(
    () => requireMCPConfigPlaceholder("@aipermission/mcp@0.2.40", "README.md"),
    /VERSION placeholder/,
  );
  assert.throws(
    () =>
      requireMCPConfigPlaceholder(
        "@aipermission/mcp@VERSION\n@aipermission/mcp@0.2.40",
        "README.md",
      ),
    /VERSION placeholder/,
  );
});

test("pinnedDockerReleaseValue rejects missing and stale image pins", () => {
  const version = "1.2.3";
  assert.equal(
    pinnedDockerReleaseValue(
      [
        `image: aipermission-backend:\${AIPERMISSION_VERSION:-${version}}`,
        `image: aipermission-backend:\${AIPERMISSION_VERSION:-${version}}`,
        `image: aipermission-frontend:\${AIPERMISSION_VERSION:-${version}}`,
      ].join("\n"),
      version,
    ),
    version,
  );
  assert.equal(
    pinnedDockerReleaseValue(
      `image: aipermission-backend:\${AIPERMISSION_VERSION:-${version}}`,
      version,
    ),
    undefined,
  );
  assert.equal(
    pinnedDockerReleaseValue(
      [
        "image: aipermission-backend:${AIPERMISSION_VERSION:-1.2.2}",
        "image: aipermission-backend:${AIPERMISSION_VERSION:-1.2.2}",
        "image: aipermission-frontend:${AIPERMISSION_VERSION:-1.2.1}",
      ].join("\n"),
      version,
    ),
    "1.2.2,1.2.2,1.2.1",
  );
});

test("commitFileUpdates restores earlier files when a later rename fails", () => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "aipermission-release-"));
  const first = path.join(directory, "first.txt");
  const second = path.join(directory, "second.txt");
  fs.writeFileSync(first, "before-first");
  fs.writeFileSync(second, "before-second");
  let failed = false;

  assert.throws(
    () =>
      commitFileUpdates(
        directory,
        new Map([
          ["first.txt", "after-first"],
          ["second.txt", "after-second"],
        ]),
        {
          renameSync(source, target) {
            if (target === second && !failed) {
              failed = true;
              throw new Error("simulated second rename failure");
            }
            fs.renameSync(source, target);
          },
        },
      ),
    /simulated second rename failure/,
  );
  assert.equal(fs.readFileSync(first, "utf8"), "before-first");
  assert.equal(fs.readFileSync(second, "utf8"), "before-second");
  assert.deepEqual(
    fs.readdirSync(directory).sort(),
    ["first.txt", "second.txt"],
  );
});

test("commitFileUpdates removes a partially written preparation file", () => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "aipermission-release-partial-"));
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

test("commitFileUpdates removes a partially written rollback file", () => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "aipermission-release-rollback-"));
  const first = path.join(directory, "first.txt");
  const second = path.join(directory, "second.txt");
  fs.writeFileSync(first, "before-first");
  fs.writeFileSync(second, "before-second");
  let writes = 0;
  let renames = 0;

  assert.throws(
    () =>
      commitFileUpdates(
        directory,
        new Map([
          ["first.txt", "after-first"],
          ["second.txt", "after-second"],
        ]),
        {
          writeFileSync(filePath, contents, options) {
            writes += 1;
            fs.writeFileSync(filePath, contents, options);
            if (writes === 3) throw new Error("simulated rollback write failure");
          },
          renameSync(source, target) {
            renames += 1;
            if (renames === 2) throw new Error("simulated commit rename failure");
            fs.renameSync(source, target);
          },
        },
      ),
    /rollback also failed/,
  );

  assert.deepEqual(fs.readdirSync(directory).sort(), ["first.txt", "second.txt"]);
});
