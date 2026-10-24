import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";
import test from "node:test";
import { isSupportedNodeRuntime, supportedNodeEngines } from "../../src/runtime-policy.js";

for (const entry of ["cli.js", "server.js"]) {
  test(`packaged ${entry} rejects a vulnerable runtime before gateway access`, () => {
    const target = new URL(`../../dist/${entry}`, import.meta.url).href;
    const result = spawnSync(
      process.execPath,
      [
        "--input-type=module",
        "--eval",
        `process.argv = ["node", ${JSON.stringify(target)}, "--help"]; Object.defineProperty(process.versions, "node", { value: "24.22.0" }); await import(${JSON.stringify(target)});`,
      ],
      {
        encoding: "utf8",
        timeout: 10000,
        env: { ...process.env, AIPERMISSION_API_TOKEN: "", AIPERMISSION_API_URL: "http://invalid.example" },
      },
    );
    assert.ifError(result.error);
    assert.equal(result.status, 1);
    assert.match(result.stderr, /Unsupported Node\.js.*24\.22\.0/);
    assert.doesNotMatch(result.stderr, /API_TOKEN|gateway URL configuration/);
  });
}

test("runtime policy excludes vulnerable patches, EOL lines and prereleases", () => {
  for (const version of ["22.23.4", "22.24.0", "24.22.1", "24.23.0", "26.12.1", "27.0.0"]) {
    assert.equal(isSupportedNodeRuntime(version), true, version);
  }
  for (const version of [
    "20.19.0",
    "22.21.0",
    "22.23.3",
    "23.99.0",
    "24.22.0",
    "25.99.0",
    "26.12.0",
    "24.22.1-rc.1",
    "v24.22.1",
    "",
    undefined,
    "99999999999999999.0.0",
  ]) {
    assert.equal(isSupportedNodeRuntime(version), false, String(version));
  }
});

test("package runtime requirements use the canonical security policy", () => {
  const metadata = JSON.parse(readFileSync(new URL("../../package.json", import.meta.url)));
  assert.equal(metadata.engines.node, supportedNodeEngines);
  assert.equal(metadata.devEngines.runtime.version, supportedNodeEngines);
});
