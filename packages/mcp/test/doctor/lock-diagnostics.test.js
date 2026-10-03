import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { constants } from "node:fs";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { promisify } from "node:util";

import { inspectClientSetup } from "../../src/doctor.js";
import { resolveSkillTarget } from "../../src/client-registry.js";
import { buildMCPServerConfig, writeProviderConfig } from "../../src/init.js";
import { installSkill } from "../../src/install-skill.js";
import { privateLockPath, withPrivateFileLock } from "../../src/private-file.js";
import { inspectPrivateLock } from "../../src/private-lock-diagnostics.js";

const execFileAsync = promisify(execFile);
const safeReadSupported = Boolean(constants.O_NOFOLLOW && constants.O_NONBLOCK);

async function fixture(t) {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-lock-diagnostic-"));
  t.after(() => fs.rm(directory, { recursive: true, force: true }));
  return directory;
}

test("new lock records carry advisory metadata without exposing the ownership token", async (t) => {
  const directory = await fixture(t);
  const file = path.join(directory, "config.json");
  assert.deepEqual(await inspectPrivateLock(file), { present: false });
  await withPrivateFileLock(file, async () => {
    const stored = JSON.parse(await fs.readFile(privateLockPath(file), "utf8"));
    const diagnostic = await inspectPrivateLock(file);
    if (!safeReadSupported) {
      assert.deepEqual(diagnostic, { present: true, readable: false });
      return;
    }
    assert.equal(diagnostic.pid, process.pid);
    assert.equal(diagnostic.readable, true);
    assert.equal(diagnostic.created_at, stored.created_at);
    assert.equal(diagnostic.process_started_at, stored.process_started_at);
    assert.ok(Date.parse(stored.process_started_at) <= Date.parse(stored.created_at));
    assert.equal(stored.hostname, os.hostname());
    assert.doesNotMatch(JSON.stringify(diagnostic), new RegExp(stored.token));
    assert.equal("token" in diagnostic, false);
  });
  assert.deepEqual(await inspectPrivateLock(file), { present: false });
});

test("legacy and malformed locks remain untouched and never disclose arbitrary contents", async (t) => {
  const directory = await fixture(t);
  const file = path.join(directory, "config.json");
  const lock = privateLockPath(file);
  for (const contents of [
    "999999\n",
    JSON.stringify({ pid: 17, token: "NEVER_DISCLOSE" }),
    "{NEVER_DISCLOSE",
    "NEVER_DISCLOSE".repeat(1000),
    JSON.stringify({ pid: 17, hostname: "NEVER_DISCLOSE\n", created_at: "NEVER_DISCLOSE", process_started_at: "2026-02-30T00:00:00.000Z" }),
  ]) {
    await fs.writeFile(lock, contents, { mode: 0o600 });
    const diagnostic = await inspectPrivateLock(file);
    assert.equal(diagnostic.present, true);
    assert.doesNotMatch(JSON.stringify(diagnostic), /NEVER_DISCLOSE|2026-02-30/);
    assert.equal(await fs.readFile(lock, "utf8"), contents);
    await assert.rejects(
      () => withPrivateFileLock(file, () => assert.fail("existing lock was stolen"), { lockRetryLimit: 1, lockRetryDelayMs: 0 }),
      /Timed out waiting/,
    );
    assert.equal(await fs.readFile(lock, "utf8"), contents);
  }
});

test("nonregular lock diagnostics do not follow links or enter directories", async (t) => {
  const directory = await fixture(t);
  const file = path.join(directory, "config.json");
  await fs.mkdir(privateLockPath(file));
  assert.deepEqual(await inspectPrivateLock(file), { present: true, readable: false });
  await fs.rmdir(privateLockPath(file));
  if (process.platform !== "win32") {
    const outside = path.join(directory, "outside");
    await fs.writeFile(outside, JSON.stringify({ pid: 17, token: "NEVER_DISCLOSE" }));
    await fs.symlink(outside, privateLockPath(file));
    assert.deepEqual(await inspectPrivateLock(file), { present: true, readable: false });
    assert.equal((await fs.lstat(privateLockPath(file))).isSymbolicLink(), true);
  }
});

test("doctor exposes a pending setup lock without modifying config, skill or ownership", async (t) => {
  const homeDir = await fixture(t);
  const config = buildMCPServerConfig({ apiUrl: "http://localhost:3210", token: "NEVER_DISCLOSE" });
  const written = await writeProviderConfig("cursor", "aipermission", config, { homeDir, scope: "user" });
  await installSkill({ client: "cursor", homeDir, scope: "user" });
  const before = await fs.readFile(written.path);
  const skill = resolveSkillTarget("cursor", "user", { homeDir });
  const skillBefore = await fs.readFile(skill.path);
  const modeBefore = (await fs.stat(written.path)).mode;
  const skillModeBefore = (await fs.stat(skill.path)).mode;
  await withPrivateFileLock(written.path, async () => {
    const lockBefore = await fs.readFile(privateLockPath(written.path));
    const result = await inspectClientSetup({ client: "cursor", homeDir, scope: "user" });
    assert.equal(result.ok, false);
    assert.equal(result.checks.filter((entry) => entry.label === "Setup lock").length, 1);
    assert.match(result.checks.at(-1).message, /Never remove while setup is running/);
    assert.doesNotMatch(JSON.stringify(result), /NEVER_DISCLOSE/);
    assert.deepEqual(await fs.readFile(written.path), before);
    assert.deepEqual(await fs.readFile(skill.path), skillBefore);
    assert.equal((await fs.stat(written.path)).mode, modeBefore);
    assert.equal((await fs.stat(skill.path)).mode, skillModeBefore);
    assert.deepEqual(await fs.readFile(privateLockPath(written.path)), lockBefore);
  });
  assert.equal((await inspectClientSetup({ client: "cursor", homeDir, scope: "user" })).ok, true);
});

test("lock diagnostic rejects replacement and growth races with a physical 4096-byte ceiling", async (t) => {
  const directory = await fixture(t);
  const file = path.join(directory, "config.json");
  const lock = privateLockPath(file);
  for (const race of ["growth", "replacement"]) {
    await fs.writeFile(lock, JSON.stringify({ pid: 17, token: "NEVER_DISCLOSE" }), { mode: 0o600 });
    let observedRead = false;
    const io = {
      lstat: fs.lstat,
      open: async (...args) => {
        const handle = await fs.open(...args);
        return {
          stat: () => handle.stat(),
          close: () => handle.close(),
          read: async (...readArgs) => {
            observedRead = true;
            assert.equal(readArgs[0].length, 4096);
            assert.equal(readArgs[2], 4096);
            if (race === "growth") await fs.appendFile(lock, " ".repeat(5000));
            else {
              await fs.rename(lock, `${lock}.original`);
              await fs.writeFile(lock, JSON.stringify({ pid: 18, token: "NEVER_DISCLOSE" }));
            }
            return handle.read(...readArgs);
          },
        };
      },
    };
    assert.deepEqual(await inspectPrivateLock(file, io), { present: true, readable: false });
    assert.equal(observedRead, safeReadSupported);
    await fs.rm(lock);
  }
});

test("lock swaps to a symlink or FIFO fail closed without blocking or following the renamed owner", async (t) => {
  const directory = await fixture(t);
  const file = path.join(directory, "config.json");
  const lock = privateLockPath(file);
  if (process.platform === "win32") {
    await fs.writeFile(lock, JSON.stringify({ pid: 17 }));
    assert.deepEqual(await inspectPrivateLock(file), { present: true, readable: false });
    return;
  }
  for (const replacement of ["symlink", "fifo"]) {
    await fs.writeFile(lock, JSON.stringify({ pid: 17, token: "NEVER_DISCLOSE" }));
    const io = {
      lstat: fs.lstat,
      open: async (...args) => {
        await fs.rename(lock, `${lock}.${replacement}`);
        if (replacement === "symlink") await fs.symlink(`${lock}.${replacement}`, lock);
        else await execFileAsync("mkfifo", [lock], { timeout: 5000 });
        return fs.open(...args);
      },
    };
    assert.deepEqual(await inspectPrivateLock(file, io), { present: true, readable: false });
    assert.equal(await fs.readFile(`${lock}.${replacement}`, "utf8"), JSON.stringify({ pid: 17, token: "NEVER_DISCLOSE" }));
    await fs.unlink(lock);
  }
});
