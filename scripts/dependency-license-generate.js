#!/usr/bin/env node

const { execFileSync } = require("node:child_process");
const fs = require("node:fs");
const path = require("node:path");
const { parseArgs } = require("node:util");
const { boundary, cacheIdentity, goRecords, moduleIdentity, readInputs, sha256 } = require("./dependency-license-inputs");

function collectEvidence(cache, record) {
  const identity = cacheIdentity(record.module, record.version);
  const directory = path.join(cache, identity);
  let files = [];
  function walk(relative) {
    for (const entry of fs
      .readdirSync(path.join(directory, relative), { withFileTypes: true })
      .sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0))) {
      const location = path.posix.join(relative, entry.name);
      if (entry.isSymbolicLink()) throw new Error(`Go evidence symlink is not allowed: ${identity}/${location}`);
      if (entry.isDirectory()) walk(location);
      else if (
        entry.isFile() &&
        /^(?:(?:licen[cs]e|copying)(?:-[a-z0-9.-]+|\.(?:txt|md|rst|bsd|lesser))?|(?:notice|patents)(?:\.(?:txt|md|rst))?)$/i.test(
          entry.name,
        )
      ) {
        const content = fs.readFileSync(path.join(directory, location));
        if (content.length) files.push({ path: `${identity}/${location}`, sha256: sha256(content), bytes: content.length });
      }
    }
  }
  let missing = "module-not-in-local-cache";
  try {
    const stat = fs.lstatSync(directory);
    if (!stat.isDirectory() || stat.isSymbolicLink()) throw new Error(`Invalid Go cache directory: ${identity}`);
  } catch (error) {
    if (error.code !== "ENOENT") throw error;
    return { status: "owner-review-required", reason: missing, files: [] };
  }
  walk("");
  missing = "no-named-license-evidence";
  files = files.sort((a, b) => (a.path < b.path ? -1 : a.path > b.path ? 1 : 0));
  return files.length ? { status: "files-recorded", files } : { status: "owner-review-required", reason: missing, files: [] };
}

function generate(root, cache, goBinary = "go") {
  if (!cache || !fs.statSync(cache).isDirectory()) throw new Error("An existing offline Go module cache is required");
  const inputs = readInputs(root);
  // edit -json parses the supplied file only; disable all Go network/toolchain resolution.
  const goMod = JSON.parse(
    execFileSync(goBinary, ["mod", "edit", "-json", path.join(root, "backend/go.mod")], {
      cwd: root,
      encoding: "utf8",
      timeout: 30_000,
      env: { ...process.env, GOPROXY: "off", GOSUMDB: "off", GOTOOLCHAIN: "local", GOWORK: "off", GOFLAGS: "" },
    }),
  );
  const go = goRecords(inputs.sums, goMod).map((record) => ({ ...record, evidence: collectEvidence(cache, record) }));
  return {
    schema_version: 1,
    boundary,
    sources: inputs.sources,
    npm: inputs.npm,
    go_mod: goMod,
    go,
    native: inputs.native,
    review: {
      status: "pending-owner-review",
      reviewer: null,
      scope: "Metadata and evidence gaps only; no legal or SPDX compatibility assertion",
      missing_go_evidence: go
        .filter((item) => item.evidence.status === "owner-review-required")
        .map((item) => ({
          identity: moduleIdentity(item.module, item.version),
          reason: item.evidence.reason,
          disposition: "pending-owner-review",
          note: "Owner must review this exact version; do not infer a license from another module or version",
        })),
    },
  };
}

function main() {
  const { values } = parseArgs({
    options: {
      root: { type: "string", default: path.resolve(__dirname, "..") },
      "go-mod-cache": { type: "string" },
      "go-binary": { type: "string", default: "go" },
      output: { type: "string" },
    },
  });
  const result = generate(path.resolve(values.root), values["go-mod-cache"], values["go-binary"]);
  const json = `${JSON.stringify(result, null, 2)}\n`;
  if (values.output) fs.writeFileSync(values.output, json);
  else process.stdout.write(json);
}

if (require.main === module) {
  try {
    main();
  } catch (error) {
    console.error(`Dependency license generation failed: ${error.message}`);
    process.exitCode = 1;
  }
}

module.exports = { collectEvidence, generate };
