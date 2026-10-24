import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { minimumRuntimeVersions, supportedNodeEngines } from "../src/runtime-policy.js";

const root = fileURLToPath(new URL("../", import.meta.url));
const version = minimumRuntimeVersions[0];
const archiveHash = "1805bcd35e6815559bd2eb97db438d3fdb0eb3a5e6a977b7c1badca76337e9ce";
assert.equal(JSON.parse(await fs.readFile(new URL("../package.json", import.meta.url), "utf8")).engines.node, supportedNodeEngines);

// Ubuntu CI exercises the published runtime floor, independently of build tooling.
if (process.platform !== "linux" || process.arch !== "x64") {
  console.log("Minimum runtime smoke runs on Linux x64 CI; native package checks remain required.");
} else {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-minimum-node-"));
  try {
    const response = await fetch(`https://nodejs.org/dist/v${version}/node-v${version}-linux-x64.tar.xz`, {
      signal: AbortSignal.timeout(60000),
    });
    assert.equal(response.ok, true, "Minimum runtime archive download failed");
    const bytes = Buffer.from(await response.arrayBuffer());
    assert.equal(createHash("sha256").update(bytes).digest("hex"), archiveHash, "Minimum runtime archive checksum changed");
    const archive = path.join(directory, "node.tar.xz");
    await fs.writeFile(archive, bytes);
    run("tar", ["-xf", archive, "-C", directory]);
    const node = path.join(directory, `node-v${version}-linux-x64/bin/node`);
    const actual = run(node, ["--version"], "pipe").stdout.trim();
    assert.equal(actual, `v${version}`);
    run(node, ["--test", "test/server.test.js", "test/transport/action-identity.test.js", "test/transport/withheld-output.test.js"]);
    console.log(`Packaged MCP minimum Node.js ${version} smoke passed.`);
  } finally {
    await fs.rm(directory, { recursive: true, force: true });
  }
}

function run(command, args, stdio = "inherit") {
  const result = spawnSync(command, args, { cwd: root, encoding: "utf8", stdio, timeout: 120000 });
  if (result.error) throw result.error;
  assert.equal(result.status, 0, `${command} failed`);
  return result;
}
