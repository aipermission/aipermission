import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { promisify } from "node:util";

import {
  atomicWritePrivateFile,
  cleanupStalePrivateFiles,
  privateLockPath,
  privateStagingIgnorePath,
  privateStagingPath,
  privateTemporaryPath,
  withPrivateFileLock,
} from "../src/private-file.js";

const execFileAsync = promisify(execFile);

test("private staging paths distinguish concrete directories from ignore patterns", () => {
  const filePath = path.join("project", "config*.json");

  assert.equal(privateStagingPath(filePath, "git-check"), path.join("project", ".config*.json.aipermission-stage-git-check"));
  assert.equal(privateStagingIgnorePath(filePath), path.join("project", ".config*.json.aipermission-stage-*"));
});

test("atomicWritePrivateFile replaces content with private permissions", async () => {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-private-file-"));
  const filePath = path.join(directory, "config.json");
  await fs.writeFile(filePath, "before", { mode: 0o644 });

  await atomicWritePrivateFile(filePath, "after", { suffix: "success" });

  assert.equal(await fs.readFile(filePath, "utf8"), "after");
  assert.equal((await fs.stat(filePath)).mode & 0o777, 0o600);
  assert.deepEqual(await fs.readdir(directory), ["config.json"]);
});

test("atomicWritePrivateFile preserves the previous file when rename fails", async () => {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-private-file-failure-"));
  const filePath = path.join(directory, "config.json");
  await fs.writeFile(filePath, "before", { mode: 0o600 });

  await assert.rejects(
    () =>
      atomicWritePrivateFile(filePath, "after", {
        suffix: "failure",
        rename: async () => {
          throw new Error("injected rename failure");
        },
      }),
    /injected rename failure/,
  );

  assert.equal(await fs.readFile(filePath, "utf8"), "before");
  assert.deepEqual(await fs.readdir(directory), ["config.json"]);
});

for (const linkType of ["relative", "absolute"]) {
  test(`atomicWritePrivateFile rejects an existing ${linkType} symbolic link`, async () => {
    const directory = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-private-file-symlink-"));
    const targetPath = path.join(directory, "managed.json");
    const filePath = path.join(directory, "config.json");
    await fs.writeFile(targetPath, "managed-before", { mode: 0o600 });
    await fs.symlink(linkType === "relative" ? path.basename(targetPath) : targetPath, filePath);

    await assert.rejects(() => atomicWritePrivateFile(filePath, "after"), /Refusing to replace symbolic-link config/);

    assert.equal(await fs.readFile(targetPath, "utf8"), "managed-before");
    assert.equal((await fs.lstat(filePath)).isSymbolicLink(), true);
  });
}

test("cleanupStalePrivateFiles removes only old validated temporary siblings", async () => {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-private-file-stale-"));
  const filePath = path.join(directory, "config.json");
  const stalePath = privateTemporaryPath(filePath, "stale");
  const freshPath = privateTemporaryPath(filePath, "fresh");
  const unrelatedPath = path.join(directory, ".config.json.unrelated.tmp");
  const staleStagingDirectory = path.join(directory, ".config.json.aipermission-stage-stale");
  await Promise.all([
    fs.writeFile(stalePath, "stale"),
    fs.writeFile(freshPath, "fresh"),
    fs.writeFile(unrelatedPath, "unrelated"),
    fs.mkdir(staleStagingDirectory),
  ]);
  await fs.writeFile(path.join(staleStagingDirectory, "config.json"), "stale secret");
  await fs.utimes(stalePath, new Date(0), new Date(0));
  await fs.utimes(staleStagingDirectory, new Date(0), new Date(0));

  await cleanupStalePrivateFiles(filePath, { now: 10_000, staleAgeMs: 5_000 });

  assert.deepEqual((await fs.readdir(directory)).sort(), [path.basename(freshPath), path.basename(unrelatedPath)].sort());
});

test("atomicWritePrivateFile reports a directory durability failure after replacement", async () => {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-private-file-sync-"));
  const filePath = path.join(directory, "config.json");
  await fs.writeFile(filePath, "before", { mode: 0o600 });

  await assert.rejects(
    () =>
      atomicWritePrivateFile(filePath, "after", {
        suffix: "sync-failure",
        syncDirectory: async () => {
          throw new Error("injected directory sync failure");
        },
      }),
    /injected directory sync failure/,
  );
  assert.equal(await fs.readFile(filePath, "utf8"), "after");
  assert.deepEqual(await fs.readdir(directory), ["config.json"]);
});

for (const linkType of ["relative", "absolute"]) {
  test(`atomicWritePrivateFile rejects a ${linkType} symbolic link in its parent chain`, async () => {
    const root = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-private-parent-"));
    const realDirectory = path.join(root, "real");
    const linkedDirectory = path.join(root, "linked");
    await fs.mkdir(realDirectory);
    await fs.symlink(linkType === "relative" ? "real" : realDirectory, linkedDirectory, "dir");
    const filePath = path.join(linkedDirectory, "config.json");

    await assert.rejects(() => atomicWritePrivateFile(filePath, "secret", { trustedRoot: root }), /symbolic link or junction/);
    await assert.rejects(() => fs.stat(path.join(realDirectory, "config.json")), { code: "ENOENT" });
  });
}

test("atomicWritePrivateFile protects the empty temporary file before writing secrets", async () => {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-private-permissions-"));
  const filePath = path.join(directory, "config.json");
  const enforced = [];

  await atomicWritePrivateFile(filePath, "secret", {
    suffix: "permissions",
    enforcePermissions: async (temporaryPath) => {
      enforced.push({ path: temporaryPath, contents: await fs.readFile(temporaryPath, "utf8") });
    },
  });

  assert.deepEqual(enforced, [{ path: privateTemporaryPath(filePath, "permissions"), contents: "" }]);
  assert.equal(await fs.readFile(filePath, "utf8"), "secret");
});

test("Windows private writes secure a staging directory before creating the secret file", async () => {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-private-windows-stage-"));
  const filePath = path.join(directory, "config.json");
  const events = [];

  await atomicWritePrivateFile(filePath, "secret", {
    platform: "win32",
    enforceDirectoryPermissions: async (stagingDirectory) => {
      events.push("directory");
      assert.deepEqual(await fs.readdir(stagingDirectory), []);
    },
    enforcePermissions: async (temporaryPath) => {
      events.push("file");
      assert.equal(await fs.readFile(temporaryPath, "utf8"), "");
    },
  });

  assert.deepEqual(events, ["directory", "file"]);
  assert.equal(await fs.readFile(filePath, "utf8"), "secret");
  assert.deepEqual(await fs.readdir(directory), ["config.json"]);
});

test("Windows ACL helpers use absolute System32 executables", async () => {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-private-windows-tools-"));
  const filePath = path.join(directory, "config.json");
  const invocations = [];

  await atomicWritePrivateFile(filePath, "secret", {
    platform: "win32",
    windowsSystemRoot: "C:\\Windows",
    execFile: async (executable, args) => {
      invocations.push({ executable, args });
      if (executable.endsWith("whoami.exe")) return { stdout: '"user","S-1-5-21-1-2-3-1001"\n', stderr: "" };
      return { stdout: "", stderr: "" };
    },
  });

  assert.ok(invocations.length >= 4);
  assert.ok(invocations.every(({ executable }) => path.win32.isAbsolute(executable)));
  assert.ok(invocations.every(({ executable }) => executable.startsWith("C:\\Windows\\System32\\")));
  assert.ok(invocations.some(({ executable }) => executable.endsWith("whoami.exe")));
  assert.ok(invocations.some(({ executable }) => executable.endsWith("icacls.exe")));
});

test("withPrivateFileLock does not steal an old lock from a live process", async () => {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-private-live-lock-"));
  const filePath = path.join(directory, "config.json");
  const lockPath = privateLockPath(filePath);
  await fs.writeFile(lockPath, `${process.pid}\n`);
  await fs.utimes(lockPath, new Date(0), new Date(0));

  await assert.rejects(
    () =>
      withPrivateFileLock(filePath, async () => {}, {
        lockRetryLimit: 1,
        lockRetryDelayMs: 0,
        now: 60_000,
        staleLockAgeMs: 1,
      }),
    /Timed out waiting for private config lock/,
  );
  assert.equal(await fs.readFile(lockPath, "utf8"), `${process.pid}\n`);
});

test("withPrivateFileLock fails closed on an old lock from a dead process", async () => {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-private-dead-lock-"));
  const filePath = path.join(directory, "config.json");
  const lockPath = privateLockPath(filePath);
  await fs.writeFile(lockPath, "999999\n");
  await fs.utimes(lockPath, new Date(0), new Date(0));
  await assert.rejects(
    () => withPrivateFileLock(filePath, async () => {}, { lockRetryLimit: 1, lockRetryDelayMs: 0 }),
    /Timed out waiting for private config lock/,
  );

  assert.equal(await fs.readFile(lockPath, "utf8"), "999999\n");
});

test("withPrivateFileLock serializes independent Node processes", async () => {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-private-process-lock-"));
  const filePath = path.join(directory, "config.json");
  const eventsPath = path.join(directory, "events.log");
  const modulePath = new URL("../src/private-file.js", import.meta.url).href;
  const worker = `
    import fs from "node:fs/promises";
    import { withPrivateFileLock } from ${JSON.stringify(modulePath)};
    const [filePath, eventsPath, id] = process.argv.slice(1);
    await withPrivateFileLock(filePath, async () => {
      await fs.appendFile(eventsPath, \`enter:\${id}\\n\`);
      await new Promise((resolve) => setTimeout(resolve, 100));
      await fs.appendFile(eventsPath, \`exit:\${id}\\n\`);
    });
  `;

  await Promise.all([
    execFileAsync(process.execPath, ["--input-type=module", "--eval", worker, filePath, eventsPath, "one"]),
    execFileAsync(process.execPath, ["--input-type=module", "--eval", worker, filePath, eventsPath, "two"]),
  ]);

  const events = (await fs.readFile(eventsPath, "utf8")).trim().split("\n");
  assert.equal(events.length, 4);
  assert.match(events[0], /^enter:(one|two)$/);
  assert.equal(events[1], events[0].replace("enter:", "exit:"));
  assert.match(events[2], /^enter:(one|two)$/);
  assert.notEqual(events[2], events[0]);
  assert.equal(events[3], events[2].replace("enter:", "exit:"));
});

test("atomicWritePrivateFile applies a protected Windows ACL", { skip: process.platform !== "win32" }, async () => {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-private-windows-acl-"));
  const filePath = path.join(directory, "config.json");

  await atomicWritePrivateFile(filePath, "secret");

  const { stdout } = await execFileAsync("icacls", [filePath, "/verify"], { encoding: "utf8", windowsHide: true });
  assert.match(stdout, /Successfully processed 1 files/i);
  const { stdout: acl } = await execFileAsync("icacls", [filePath], { encoding: "utf8", windowsHide: true });
  assert.match(acl, /\(F\)/);
  assert.doesNotMatch(acl, /\(I\)/);
});
