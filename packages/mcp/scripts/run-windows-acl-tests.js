import { spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { loadAndVerifyTestManifest } from "./test-manifest-policy.js";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const manifest = loadAndVerifyTestManifest();
const source = fs.readFileSync(path.join(root, "test/private-file.test.js"), "utf8");
const declaredACLTests = [...source.matchAll(/test\("([^"]*ACL[^"]*)"/g)].map((match) => match[1]).sort();
const expectedACLTests = [...manifest.windowsACLTests].sort();
if (JSON.stringify(declaredACLTests) !== JSON.stringify(expectedACLTests)) {
  throw new Error(
    `Windows ACL manifest mismatch:\nexpected ${JSON.stringify(declaredACLTests)}\nreceived ${JSON.stringify(expectedACLTests)}`,
  );
}
for (const name of manifest.windowsACLTests) {
  if (!source.includes(`test("${name}"`)) throw new Error(`Missing required Windows ACL test: ${name}`);
}
const exactTestPattern = `^(?:${manifest.windowsACLTests.map((name) => name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")).join("|")})$`;
const result = spawnSync(process.execPath, ["--test", `--test-name-pattern=${exactTestPattern}`, "test/private-file.test.js"], {
  cwd: root,
  encoding: "utf8",
  stdio: ["ignore", "pipe", "pipe"],
});
process.stdout.write(result.stdout || "");
process.stderr.write(result.stderr || "");
if (result.error) throw result.error;
if (result.status !== 0) process.exit(result.status || 1);
const passMatch = /^# pass (\d+)$/m.exec(result.stdout);
const passed = passMatch ? Number(passMatch[1]) : 0;
if (passed !== manifest.windowsACLTests.length) {
  throw new Error(`Windows ACL suite passed ${passed} tests; expected ${manifest.windowsACLTests.length}`);
}
