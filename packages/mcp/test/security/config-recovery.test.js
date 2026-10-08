import assert from "node:assert/strict";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { atomicWritePrivateFile } from "../../src/private-file.js";
import { readPrivateFileSnapshot } from "../../src/private-file-snapshot.js";
import { privateRecoveryDirectory } from "../../src/private-file-recovery.js";

async function fixture(t) {
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-config-recovery-"));
  t.after(() => fs.rm(dir, { recursive: true, force: true }));
  return path.join(dir, "config");
}

test("private recovery retains the previous source and rotates one slot", async (t) => {
  const file = await fixture(t);
  await fs.writeFile(file, "before", { mode: 0o644 });
  const writer = await fs.open(file, "r+");
  t.after(() => writer.close());
  const result = await atomicWritePrivateFile(file, "gateway", {
    expectedSnapshot: await readPrivateFileSnapshot(file),
    rename: async (source, destination) => {
      await writer.writeFile("edited");
      await fs.rename(source, destination);
    },
  });
  await writer.close();
  assert.equal(await fs.readFile(result.recoveryPath, "utf8"), process.platform === "win32" ? "before" : "edited");
  assert.equal(await fs.readFile(file, "utf8"), "gateway");
  if (process.platform !== "win32") {
    assert.equal((await fs.stat(result.recoveryPath)).mode & 0o077, 0);
    assert.equal((await fs.stat(privateRecoveryDirectory(file))).mode & 0o077, 0);
  }
  await atomicWritePrivateFile(file, "next", { expectedSnapshot: await readPrivateFileSnapshot(file) });
  assert.equal(await fs.readFile(result.recoveryPath, "utf8"), "gateway");
  assert.deepEqual(await fs.readdir(privateRecoveryDirectory(file)), ["previous"]);
});

test("private recovery survives failed destination replacement", async (t) => {
  const file = await fixture(t);
  await fs.writeFile(file, "before", { mode: 0o600 });
  await assert.rejects(
    async () =>
      atomicWritePrivateFile(file, "gateway", {
        expectedSnapshot: await readPrivateFileSnapshot(file),
        rename: async () => {
          throw new Error("injected replace failure");
        },
      }),
    /injected replace failure/,
  );
  assert.equal(await fs.readFile(file, "utf8"), "before");
  assert.equal(await fs.readFile(path.join(privateRecoveryDirectory(file), "previous"), "utf8"), "before");
  await atomicWritePrivateFile(file, "retried", { expectedSnapshot: await readPrivateFileSnapshot(file) });
  assert.deepEqual(await fs.readdir(privateRecoveryDirectory(file)), ["previous"]);
  await atomicWritePrivateFile(file, "subsequent", { expectedSnapshot: await readPrivateFileSnapshot(file) });
  assert.equal(await fs.readFile(file, "utf8"), "subsequent");
  assert.equal(await fs.readFile(path.join(privateRecoveryDirectory(file), "previous"), "utf8"), "retried");
});

test("a fresh config is published without overwriting a late external creation", async (t) => {
  const file = await fixture(t);
  await assert.rejects(
    async () =>
      atomicWritePrivateFile(file, "gateway", {
        expectedSnapshot: await readPrivateFileSnapshot(file),
        link: async (source, destination) => {
          await fs.writeFile(destination, "external", { mode: 0o600 });
          await fs.link(source, destination);
        },
      }),
    { code: "AIPERMISSION_CONFIG_CHANGED" },
  );
  assert.equal(await fs.readFile(file, "utf8"), "external");
  assert.deepEqual(await fs.readdir(path.dirname(file)), ["config"]);
});

test("snapshot-only recovery does not follow a replaced original inode", async (t) => {
  const file = await fixture(t);
  await fs.writeFile(file, "before", { mode: 0o600 });
  const written = await atomicWritePrivateFile(file, "gateway", {
    expectedSnapshot: await readPrivateFileSnapshot(file),
    platform: "win32",
    enforcePermissions: (target) => fs.chmod(target, 0o600),
    enforceDirectoryPermissions: (target) => fs.chmod(target, 0o700),
    rename: async (source, destination) => {
      await fs.unlink(destination);
      await fs.writeFile(destination, "external replacement", { mode: 0o600 });
      await fs.rename(source, destination);
    },
  });
  assert.equal(await fs.readFile(written.recoveryPath, "utf8"), "before");
  assert.equal(await fs.readFile(file, "utf8"), "gateway");
});

test("a foreign recovery candidate is not overwritten", async (t) => {
  const file = await fixture(t);
  await fs.writeFile(file, "before", { mode: 0o600 });
  await fs.mkdir(privateRecoveryDirectory(file), { mode: 0o700 });
  const candidate = path.join(privateRecoveryDirectory(file), "candidate");
  await fs.writeFile(candidate, "foreign recovery", { mode: 0o600 });
  await assert.rejects(
    async () =>
      atomicWritePrivateFile(file, "gateway", {
        expectedSnapshot: await readPrivateFileSnapshot(file),
      }),
    { code: "EEXIST" },
  );
  assert.equal(await fs.readFile(candidate, "utf8"), "foreign recovery");
  assert.equal(await fs.readFile(file, "utf8"), "before");
});

test("snapshot-copy recovery preserves original bytes without decoding", async (t) => {
  const file = await fixture(t);
  const bytes = Buffer.from([0x7b, 0x22, 0x78, 0x22, 0x3a, 0x22, 0xe9, 0x22, 0x7d]);
  await fs.writeFile(file, bytes, { mode: 0o600 });
  const written = await atomicWritePrivateFile(file, "{}", {
    expectedSnapshot: await readPrivateFileSnapshot(file),
    platform: "win32",
    enforcePermissions: (target) => fs.chmod(target, 0o600),
    enforceDirectoryPermissions: (target) => fs.chmod(target, 0o700),
  });
  assert.deepEqual(await fs.readFile(written.recoveryPath), bytes);
});

test("fresh POSIX config creation falls back to an exclusive private write without hard links", async (t) => {
  const file = await fixture(t);
  await atomicWritePrivateFile(file, "gateway", {
    expectedSnapshot: await readPrivateFileSnapshot(file),
    platform: "linux",
    link: async () => {
      throw Object.assign(new Error("unsupported"), { code: "ENOTSUP" });
    },
  });
  assert.equal(await fs.readFile(file, "utf8"), "gateway");
  if (process.platform !== "win32") assert.equal((await fs.stat(file)).mode & 0o077, 0);
  assert.deepEqual(await fs.readdir(path.dirname(file)), ["config"]);
});

test("exclusive fallback preserves a late external config creation", async (t) => {
  const file = await fixture(t);
  await assert.rejects(
    async () =>
      atomicWritePrivateFile(file, "gateway", {
        expectedSnapshot: await readPrivateFileSnapshot(file),
        platform: "linux",
        link: async () => {
          await fs.writeFile(file, "external", { mode: 0o600 });
          throw Object.assign(new Error("unsupported"), { code: "ENOTSUP" });
        },
      }),
    { code: "AIPERMISSION_CONFIG_CHANGED" },
  );
  assert.equal(await fs.readFile(file, "utf8"), "external");
});

test("unsupported Windows creation fails closed with an empty-config retry path", async (t) => {
  const file = await fixture(t);
  await assert.rejects(
    async () =>
      atomicWritePrivateFile(file, "gateway", {
        expectedSnapshot: await readPrivateFileSnapshot(file),
        platform: "win32",
        enforcePermissions: (target) => fs.chmod(target, 0o600),
        enforceDirectoryPermissions: (target) => fs.chmod(target, 0o700),
        link: async () => {
          throw Object.assign(new Error("unsupported"), { code: "ENOTSUP" });
        },
      }),
    { code: "AIPERMISSION_CONFIG_CREATION_UNSUPPORTED" },
  );
  assert.deepEqual(await fs.readdir(path.dirname(file)), []);
});

test("exclusive fallback reports an interrupted private file without claiming success", async (t) => {
  const file = await fixture(t);
  const originalOpen = fs.open.bind(fs);
  t.mock.method(fs, "open", async (target, ...args) => {
    const handle = await originalOpen(target, ...args);
    if (target === file && args[0] === "wx") {
      const write = handle.writeFile.bind(handle);
      t.mock.method(handle, "writeFile", async () => {
        await write("part");
        throw new Error("injected write failure");
      });
    }
    return handle;
  });
  await assert.rejects(
    async () =>
      atomicWritePrivateFile(file, "gateway", {
        expectedSnapshot: await readPrivateFileSnapshot(file),
        platform: "linux",
        link: async () => {
          throw Object.assign(new Error("unsupported"), { code: "ENOTSUP" });
        },
      }),
    { code: "AIPERMISSION_CONFIG_CREATION_INCOMPLETE" },
  );
  assert.equal(await fs.readFile(file, "utf8"), "part");
  if (process.platform !== "win32") assert.equal((await fs.stat(file)).mode & 0o077, 0);
});
