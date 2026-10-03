import { constants } from "node:fs";
import fs from "node:fs/promises";

export async function readPrivateFileSnapshot(filePath) {
  let handle;
  try {
    handle = await fs.open(filePath, constants.O_RDONLY | (constants.O_NOFOLLOW ?? 0) | (constants.O_NONBLOCK ?? 0));
    const before = await handle.stat({ bigint: true });
    if (!before.isFile()) throw new Error("Private config must be a regular file");
    const bytes = await handle.readFile();
    const after = await handle.stat({ bigint: true });
    if (!sameIdentity(before, after) || BigInt(bytes.length) !== after.size) throw snapshotConflict(filePath);
    return { bytes, identity: after, content: bytes.toString("utf8") };
  } catch (error) {
    if (error.code === "ENOENT") return { bytes: null, identity: null, content: "" };
    throw error;
  } finally {
    await handle?.close();
  }
}

export async function assertPrivateFileUnchanged(filePath, expected) {
  const current = await readPrivateFileSnapshot(filePath);
  const sameBytes = expected.bytes === null ? current.bytes === null : current.bytes !== null && expected.bytes.equals(current.bytes);
  if (!sameBytes || !sameIdentity(expected.identity, current.identity)) throw snapshotConflict(filePath);
}

function sameIdentity(before, after) {
  if (before === null || after === null) return before === after;
  return ["dev", "ino", "size", "mtimeNs", "ctimeNs", "mode"].every((field) => before[field] === after[field]);
}

function snapshotConflict(filePath) {
  const error = new Error(`Config changed while preparing an update: ${filePath}. Review the current file and retry.`);
  error.code = "AIPERMISSION_CONFIG_CHANGED";
  return error;
}
