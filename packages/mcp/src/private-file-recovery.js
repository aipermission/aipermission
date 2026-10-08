import fs from "node:fs/promises";
import { constants } from "node:fs";
import path from "node:path";
import { readPrivateFileSnapshot } from "./private-file-snapshot.js";

export function privateRecoveryDirectory(filePath) {
  return path.join(path.dirname(filePath), `.${path.basename(filePath)}.aipermission-recovery`);
}

export async function retainPrivateFile(filePath, expected, options, helpers) {
  const directory = privateRecoveryDirectory(filePath);
  const candidate = path.join(directory, "candidate");
  const previous = path.join(directory, "previous");
  await helpers.prepareDirectory(directory);
  await helpers.validate(previous);
  let ownedCandidate = false;
  let handle;
  const copy = async () => {
    const empty = await fs.open(candidate, "wx", 0o600);
    ownedCandidate = true;
    await empty.close();
    await helpers.writeCopy(candidate, expected.bytes);
  };
  try {
    if (options.platform !== "win32" && process.platform !== "win32" && expected.identity.uid === BigInt(process.getuid())) {
      let linkedInode = false;
      try {
        await fs.link(filePath, candidate);
        linkedInode = true;
        ownedCandidate = true;
      } catch (error) {
        if (!["EPERM", "EOPNOTSUPP", "ENOTSUP", "EXDEV", "ENOSYS"].includes(error.code)) throw error;
        await copy();
      }
      if (linkedInode) {
        const linked = await readPrivateFileSnapshot(candidate);
        if (!sameSource(expected, linked)) throw recoveryConflict(filePath);
        handle = await fs.open(candidate, constants.O_RDONLY | (constants.O_NOFOLLOW ?? 0) | (constants.O_NONBLOCK ?? 0));
        const stat = await handle.stat({ bigint: true });
        if (!stat.isFile() || stat.dev !== linked.identity.dev || stat.ino !== linked.identity.ino) throw recoveryConflict(filePath);
        await handle.chmod(0o600);
        await handle.sync();
        await handle.close();
        handle = undefined;
      }
    } else {
      await copy();
    }
    const retained = await fs.lstat(previous, { bigint: true }).catch((error) => {
      if (error.code === "ENOENT") return null;
      throw error;
    });
    const staged = await fs.lstat(candidate, { bigint: true });
    // POSIX rename is a no-op when both names already reference the same inode.
    if (retained && retained.dev === staged.dev && retained.ino === staged.ino) await fs.unlink(candidate);
    else await fs.rename(candidate, previous);
    ownedCandidate = false;
    await helpers.syncDirectory(directory);
    const current = await readPrivateFileSnapshot(filePath);
    if (!sameSource(expected, current)) throw recoveryConflict(filePath);
    return { snapshot: current, recoveryPath: previous };
  } finally {
    await handle?.close().catch(() => {});
    if (ownedCandidate) await fs.unlink(candidate).catch(() => {});
  }
}

function sameSource(before, after) {
  return (
    before.bytes !== null &&
    after.bytes !== null &&
    before.bytes.equals(after.bytes) &&
    ["dev", "ino", "size", "mtimeNs"].every((field) => before.identity[field] === after.identity?.[field])
  );
}

function recoveryConflict(filePath) {
  const error = new Error(`Config changed while retaining recovery: ${filePath}. Review the current file and retry.`);
  error.code = "AIPERMISSION_CONFIG_CHANGED";
  return error;
}
