import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import { fileURLToPath } from "node:url";
import test from "node:test";

test("placeholder npm artifact includes the canonical AGPL license", () => {
  const root = new URL("../../../../", import.meta.url);
  const directory = new URL("packages/npm-placeholder/", root);
  const license = fs.readFileSync(new URL("LICENSE", directory), "utf8");
  assert.equal(license, fs.readFileSync(new URL("LICENSE", root), "utf8"));
  const output = execFileSync(process.platform === "win32" ? "npm.cmd" : "npm", ["pack", "--dry-run", "--json", "--ignore-scripts"], {
    cwd: fileURLToPath(directory),
    encoding: "utf8",
    timeout: 30000,
    shell: process.platform === "win32",
  });
  const [artifact] = JSON.parse(output);
  assert.deepEqual(artifact.files.map((file) => file.path).sort(), ["LICENSE", "README.md", "cli.js", "package.json"]);
});
