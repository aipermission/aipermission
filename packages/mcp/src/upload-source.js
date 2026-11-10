import { constants } from "node:fs";
import { open, readFile, readlink } from "node:fs/promises";
import { createHash } from "node:crypto";
import path from "node:path";

const directoryFlags = constants.O_RDONLY | constants.O_DIRECTORY | constants.O_NOFOLLOW;
const fileFlags = constants.O_RDONLY | constants.O_NOFOLLOW | constants.O_NONBLOCK;
const chunkBytes = 64 << 10;

export function parseUploadRoots(raw) {
  if (!raw) return [];
  let roots;
  try {
    roots = JSON.parse(raw);
  } catch {
    throw new Error("AIPERMISSION_UPLOAD_ROOTS must be a JSON array of absolute directories.");
  }
  if (!Array.isArray(roots) || roots.length > 16 || !roots.every((root) => canonicalPath(root) && root !== "/")) {
    throw new Error("AIPERMISSION_UPLOAD_ROOTS must contain up to 16 canonical absolute directories; '/' is not allowed.");
  }
  return roots;
}

function canonicalPath(value) {
  return (
    typeof value === "string" &&
    value.startsWith("/") &&
    !value.includes("\0") &&
    path.posix.normalize(value) === value &&
    (value === "/" || !value.endsWith("/"))
  );
}

function below(root, value) {
  return value.startsWith(`${root}/`);
}

export async function openUploadSource(localPath, roots, limit) {
  if (!roots.length) throw new Error("Local uploads are disabled. Configure AIPERMISSION_UPLOAD_ROOTS on the MCP client.");
  if (process.platform !== "linux") throw new Error("Opening local upload files currently requires Linux with /proc/self/fd.");
  const root = canonicalPath(localPath) && [...roots].sort((a, b) => b.length - a.length).find((candidate) => below(candidate, localPath));
  if (!root) throw new Error("local_path must be an absolute file path inside an approved upload directory.");

  let handle;
  try {
    handle = await openUploadDescriptor(root, localPath);
    const stat = await handle.stat({ bigint: true });
    if (!stat.isFile() || stat.nlink !== 1n || (await readlink(`/proc/self/fd/${handle.fd}`)) !== localPath) {
      throw new Error("Upload source is not a stable, single-link regular file.");
    }
    if (stat.size > BigInt(limit)) throw new Error("Local file exceeds the connector upload limit.");
    return new UploadSource(handle, stat, localPath);
  } catch (error) {
    await handle?.close();
    if (typeof error?.code === "string")
      throw new Error("Cannot open the local upload file. Symlinks and inaccessible files are not allowed.", { cause: error });
    throw error;
  }
}

// Walk from an open parent descriptor so replacing any path component with a
// symlink cannot redirect a subsequent open outside the approved directory.
async function openUploadDescriptor(root, localPath) {
  const components = localPath.slice(1).split("/");
  const basename = components.pop();
  const rootDepth = root.slice(1).split("/").length;
  let rootMount;
  let parent = await open("/", directoryFlags);
  try {
    for (const [index, name] of components.entries()) {
      const child = await open(`/proc/self/fd/${parent.fd}/${name}`, directoryFlags);
      const previous = parent;
      parent = child;
      await previous.close();
      if (index + 1 === rootDepth) rootMount = await mountID(parent);
      if (index + 1 > rootDepth) await requireSameMount(parent, rootMount);
    }
    const file = await open(`/proc/self/fd/${parent.fd}/${basename}`, fileFlags);
    try {
      await requireSameMount(file, rootMount);
      return file;
    } catch (error) {
      await file.close();
      throw error;
    }
  } finally {
    await parent.close();
  }
}

async function mountID(handle) {
  const info = await readFile(`/proc/self/fdinfo/${handle.fd}`, "utf8");
  const id = /^mnt_id:\s*(\d+)$/m.exec(info)?.[1];
  if (!id) throw new Error("Cannot verify the local upload filesystem mount.");
  return id;
}

async function requireSameMount(handle, expected) {
  if ((await mountID(handle)) !== expected) throw new Error("Upload paths must not cross mounts below an approved directory.");
}

class UploadSource {
  constructor(handle, stat, localPath) {
    this.handle = handle;
    this.stat = stat;
    this.localPath = localPath;
    this.size = Number(stat.size);
    this.name = path.posix.basename(localPath);
  }

  async checksum(signal) {
    const hash = createHash("sha256");
    for await (const bytes of this.chunks(signal)) hash.update(bytes);
    return hash.digest("hex");
  }

  async *chunks(signal) {
    await this.checkUnchanged();
    for (let offset = 0; offset < this.size;) {
      signal?.throwIfAborted();
      const chunk = Buffer.allocUnsafe(Math.min(chunkBytes, this.size - offset));
      const { bytesRead } = await this.handle.read(chunk, 0, chunk.length, offset);
      if (!bytesRead) throw new Error("The local upload file changed while being read.");
      offset += bytesRead;
      yield chunk.subarray(0, bytesRead);
    }
    signal?.throwIfAborted();
    await this.checkUnchanged();
  }

  async checkUnchanged() {
    const stat = await this.handle.stat({ bigint: true });
    if (
      stat.size !== this.stat.size ||
      stat.nlink !== 1n ||
      stat.mtimeNs !== this.stat.mtimeNs ||
      stat.ctimeNs !== this.stat.ctimeNs ||
      (await readlink(`/proc/self/fd/${this.handle.fd}`)) !== this.localPath
    ) {
      throw new Error("The local upload file changed while being read.");
    }
  }

  close() {
    return this.handle.close();
  }
}
