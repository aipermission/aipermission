import { constants } from "node:fs";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";

const maximumRecordBytes = 4096;

export function privateLockOwner(token) {
  return {
    pid: process.pid,
    token,
    hostname: os.hostname(),
    created_at: new Date().toISOString(),
    process_started_at: new Date(performance.timeOrigin).toISOString(),
  };
}

// Read-only metadata is advisory: timestamps and PID alone never authorize removal.
export async function inspectPrivateLock(filePath, io = fs) {
  const lockPath = `${path.resolve(filePath)}.aipermission.lock`;
  let handle;
  try {
    const before = await io.lstat(lockPath);
    if (!before.isFile() || before.size > maximumRecordBytes) return { present: true, readable: false };
    if (!constants.O_NOFOLLOW || !constants.O_NONBLOCK) return { present: true, readable: false };
    handle = await io.open(lockPath, constants.O_RDONLY | constants.O_NOFOLLOW | constants.O_NONBLOCK);
    const opened = await handle.stat();
    if (!opened.isFile() || opened.dev !== before.dev || opened.ino !== before.ino || opened.size > maximumRecordBytes) {
      return { present: true, readable: false };
    }
    const buffer = Buffer.alloc(maximumRecordBytes);
    const { bytesRead } = await handle.read(buffer, 0, buffer.length, 0);
    const after = await handle.stat();
    const current = await io.lstat(lockPath);
    if (bytesRead !== before.size || !sameRecord(before, after) || !sameRecord(before, current)) return { present: true, readable: false };
    const record = JSON.parse(buffer.subarray(0, bytesRead).toString("utf8"));
    const pid = typeof record === "number" ? record : record?.pid;
    if (!Number.isSafeInteger(pid) || pid < 1) return { present: true, readable: false };
    return {
      present: true,
      readable: true,
      pid,
      hostname: typeof record?.hostname === "string" && /^[A-Za-z0-9._-]{1,255}$/.test(record.hostname) ? record.hostname : null,
      created_at: safeTimestamp(record?.created_at),
      process_started_at: safeTimestamp(record?.process_started_at),
    };
  } catch (error) {
    if (error.code === "ENOENT") return { present: false };
    return { present: true, readable: false };
  } finally {
    await handle?.close();
  }
}

function sameRecord(before, after) {
  return after.isFile() && ["dev", "ino", "size", "mtimeMs", "ctimeMs"].every((field) => before[field] === after[field]);
}

function safeTimestamp(value) {
  if (typeof value !== "string" || !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/.test(value)) return null;
  const time = Date.parse(value);
  return Number.isFinite(time) && new Date(time).toISOString() === value ? value : null;
}
