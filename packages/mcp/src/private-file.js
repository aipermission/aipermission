import { execFile } from "node:child_process";
import { randomBytes } from "node:crypto";
import fs from "node:fs/promises";
import path from "node:path";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);
const staleTemporaryAgeMs = 24 * 60 * 60 * 1000;
const lockRetryDelayMs = 50;
const lockRetryLimit = 100;

export async function atomicWritePrivateFile(filePath, contents, options = {}) {
  const destination = path.resolve(filePath);
  const rename = options.rename || fs.rename;
  const suffix = options.suffix || `${process.pid}-${randomBytes(8).toString("hex")}`;
  const directory = path.dirname(destination);
  let temporaryPath = privateTemporaryPath(destination, suffix);
  let stagingDirectory;
  let handle;

  await ensurePrivateDirectory(directory, options);
  await validatePrivateDestination(destination, options);
  await cleanupStalePrivateFiles(destination, options);
  try {
    if (operatingSystem(options) === "win32") {
      stagingDirectory = await createPrivateStagingDirectory(destination, options);
      temporaryPath = path.join(stagingDirectory, path.basename(temporaryPath));
    }
    handle = await fs.open(temporaryPath, "wx", 0o600);
    await enforcePrivateFilePermissions(temporaryPath, options);
    await handle.writeFile(contents, { encoding: "utf8" });
    await handle.sync();
    await handle.close();
    handle = undefined;
    await validatePrivateDestination(destination, options);
    await rename(temporaryPath, destination);
    await (options.syncDirectory || syncParentDirectory)(directory);
  } catch (error) {
    await handle?.close().catch(() => {});
    await fs.unlink(temporaryPath).catch(() => {});
    throw error;
  } finally {
    if (stagingDirectory) await fs.rm(stagingDirectory, { recursive: true, force: true }).catch(() => {});
  }
}

export async function withPrivateFileLock(filePath, task, options = {}) {
  const destination = path.resolve(filePath);
  const directory = path.dirname(destination);
  const lockPath = `${destination}.aipermission.lock`;
  await ensurePrivateDirectory(directory, options);
  await validatePrivateDestination(destination, options);

  const ownerToken = randomBytes(16).toString("hex");
  const ownerRecord = JSON.stringify({ pid: process.pid, token: ownerToken });
  let handle;
  for (let attempt = 0; attempt < (options.lockRetryLimit ?? lockRetryLimit); attempt += 1) {
    let createdLock = false;
    try {
      handle = await fs.open(lockPath, "wx", 0o600);
      createdLock = true;
      await enforcePrivateFilePermissions(lockPath, options);
      await handle.writeFile(`${ownerRecord}\n`, { encoding: "utf8" });
      await handle.sync();
      break;
    } catch (error) {
      await handle?.close().catch(() => {});
      handle = undefined;
      if (createdLock) await fs.unlink(lockPath).catch(() => {});
      if (error.code !== "EEXIST") throw error;
      await delay(options.lockRetryDelayMs ?? lockRetryDelayMs);
    }
  }
  if (!handle) {
    throw new Error(
      `Timed out waiting for private config lock: ${destination}. If no other MCP setup process is running, remove ${lockPath} and retry.`,
    );
  }

  try {
    return await task();
  } finally {
    await handle.close().catch(() => {});
    await releaseOwnedLock(lockPath, ownerToken);
    await (options.syncDirectory || syncParentDirectory)(directory);
  }
}

export function privateTemporaryPath(filePath, suffix) {
  return path.join(path.dirname(filePath), `.${path.basename(filePath)}.aipermission-${suffix}.tmp`);
}

export function privateTemporaryIgnorePath(filePath) {
  return privateTemporaryPath(filePath, "*");
}

export function privateStagingPath(filePath, suffix) {
  return path.join(path.dirname(filePath), `.${path.basename(filePath)}.aipermission-stage-${suffix}`);
}

export function privateStagingIgnorePath(filePath) {
  return privateStagingPath(filePath, "*");
}

export function privateLockPath(filePath) {
  return `${filePath}.aipermission.lock`;
}

export async function cleanupStalePrivateFiles(filePath, options = {}) {
  const directory = path.dirname(filePath);
  const prefix = `.${path.basename(filePath)}.aipermission-`;
  const now = options.now ?? Date.now();
  const maximumAge = options.staleAgeMs ?? staleTemporaryAgeMs;
  let entries;
  try {
    entries = await fs.readdir(directory, { withFileTypes: true });
  } catch (error) {
    if (error.code === "ENOENT") return;
    throw error;
  }
  for (const entry of entries) {
    const temporaryFile = entry.isFile() && entry.name.startsWith(prefix) && entry.name.endsWith(".tmp");
    const stagingDirectory = entry.isDirectory() && entry.name.startsWith(`${prefix}stage-`);
    if (!temporaryFile && !stagingDirectory) continue;
    const temporaryPath = path.join(directory, entry.name);
    try {
      const stat = await fs.lstat(temporaryPath);
      if (now - stat.mtimeMs < maximumAge) continue;
      if (stat.isFile()) await fs.unlink(temporaryPath);
      if (stat.isDirectory()) await fs.rm(temporaryPath, { recursive: true, force: true });
    } catch (error) {
      if (error.code !== "ENOENT") throw error;
    }
  }
}

async function ensurePrivateDirectory(directory, options) {
  const trustedRoot = path.resolve(options.trustedRoot || directory);
  const resolvedDirectory = path.resolve(directory);
  assertPathWithinRoot(trustedRoot, resolvedDirectory);
  const missing = [];
  const relativeParts = path.relative(trustedRoot, resolvedDirectory).split(path.sep).filter(Boolean);
  let current = trustedRoot;
  for (const part of ["", ...relativeParts]) {
    if (part) current = path.join(current, part);
    try {
      const stat = await fs.lstat(current);
      if (stat.isSymbolicLink()) throw new Error(`Refusing private config path through symbolic link or junction: ${current}`);
      if (!stat.isDirectory()) throw new Error(`Private config parent is not a directory: ${current}`);
    } catch (error) {
      if (error.code !== "ENOENT") throw error;
      missing.push(current);
    }
  }
  await fs.mkdir(resolvedDirectory, { recursive: true, mode: 0o700 });
  await rejectSymbolicPathComponents(trustedRoot, resolvedDirectory);
  for (const created of missing) {
    await fs.chmod(created, 0o700).catch((error) => {
      if (process.platform !== "win32") throw error;
    });
    await (options.syncDirectory || syncParentDirectory)(path.dirname(created));
  }
}

async function createPrivateStagingDirectory(destination, options) {
  const prefix = path.join(path.dirname(destination), `.${path.basename(destination)}.aipermission-stage-`);
  const directory = await (options.makeTemporaryDirectory || fs.mkdtemp)(prefix);
  try {
    await enforcePrivateDirectoryPermissions(directory, options);
    return directory;
  } catch (error) {
    await fs.rm(directory, { recursive: true, force: true }).catch(() => {});
    throw error;
  }
}

async function validatePrivateDestination(filePath, options) {
  const trustedRoot = path.resolve(options.trustedRoot || path.dirname(filePath));
  assertPathWithinRoot(trustedRoot, filePath);
  await rejectSymbolicPathComponents(trustedRoot, path.dirname(filePath));
  await rejectSymbolicLink(filePath);
}

async function rejectSymbolicPathComponents(root, directory) {
  const relativeParts = path.relative(root, directory).split(path.sep).filter(Boolean);
  let current = root;
  for (const part of ["", ...relativeParts]) {
    if (part) current = path.join(current, part);
    const stat = await fs.lstat(current);
    if (stat.isSymbolicLink()) {
      throw new Error(`Refusing private config path through symbolic link or junction: ${current}`);
    }
    if (!stat.isDirectory()) {
      throw new Error(`Private config parent is not a directory: ${current}`);
    }
  }
}

function assertPathWithinRoot(root, destination) {
  const relative = path.relative(path.resolve(root), path.resolve(destination));
  if (relative.startsWith(`..${path.sep}`) || relative === ".." || path.isAbsolute(relative)) {
    throw new Error(`Refusing private config path outside trusted root: ${destination}`);
  }
}

async function rejectSymbolicLink(filePath) {
  try {
    const stat = await fs.lstat(filePath);
    if (stat.isSymbolicLink()) {
      throw new Error(`Refusing to replace symbolic-link config: ${filePath}`);
    }
  } catch (error) {
    if (error.code === "ENOENT") return;
    throw error;
  }
}

async function releaseOwnedLock(lockPath, ownerToken) {
  try {
    const owner = JSON.parse(await fs.readFile(lockPath, "utf8"));
    if (owner?.token !== ownerToken) return;
    await fs.unlink(lockPath);
  } catch (error) {
    if (error.code === "ENOENT" || error instanceof SyntaxError) return;
    throw error;
  }
}

async function enforcePrivateFilePermissions(filePath, options) {
  if (options.enforcePermissions) {
    await options.enforcePermissions(filePath);
    return;
  }
  if (operatingSystem(options) !== "win32") {
    await fs.chmod(filePath, 0o600);
    return;
  }
  const sid = await currentWindowsSID(options);
  await runWindowsSystemExecutable("icacls", [filePath, "/inheritance:r", "/grant:r", `*${sid}:(F)`], options);
}

async function enforcePrivateDirectoryPermissions(directory, options) {
  if (options.enforceDirectoryPermissions) {
    await options.enforceDirectoryPermissions(directory);
    return;
  }
  if (operatingSystem(options) !== "win32") {
    await fs.chmod(directory, 0o700);
    return;
  }
  const sid = await currentWindowsSID(options);
  await runWindowsSystemExecutable("icacls", [directory, "/inheritance:r", "/grant:r", `*${sid}:(OI)(CI)(F)`], options);
}

async function currentWindowsSID(options) {
  const { stdout } = await runWindowsSystemExecutable("whoami", ["/user", "/fo", "csv", "/nh"], options, {
    encoding: "utf8",
  });
  const sid = stdout.match(/S-\d-(?:\d+-)+\d+/)?.[0];
  if (!sid) throw new Error("Could not determine current Windows SID for private config ACL");
  return sid;
}

function runWindowsSystemExecutable(name, args, options, executionOptions = {}) {
  const execute = options.execFile || execFileAsync;
  return execute(windowsSystemExecutable(name, options), args, {
    windowsHide: true,
    ...executionOptions,
  });
}

function windowsSystemExecutable(name, options) {
  const systemRoot = options.windowsSystemRoot || process.env.SystemRoot || process.env.windir || "C:\\Windows";
  if (!path.win32.isAbsolute(systemRoot)) {
    throw new Error("Windows SystemRoot must be an absolute path for private config ACL setup");
  }
  return path.win32.join(systemRoot, "System32", `${name}.exe`);
}

function operatingSystem(options) {
  return options.platform || process.platform;
}

async function syncParentDirectory(directory) {
  let handle;
  try {
    handle = await fs.open(directory, "r");
    await handle.sync();
  } catch (error) {
    if (process.platform === "win32" && ["EACCES", "EINVAL", "ENOTSUP", "EPERM"].includes(error.code)) return;
    throw error;
  } finally {
    await handle?.close().catch(() => {});
  }
}

function delay(milliseconds) {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}
