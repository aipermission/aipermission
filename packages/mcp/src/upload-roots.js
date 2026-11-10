import { homedir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { RootsListChangedNotificationSchema } from "@modelcontextprotocol/sdk/types.js";
import { parseUploadRoots } from "./upload-source.js";

export function createUploadRootResolver(server, { configuredRoots, cwd = process.cwd(), home = homedir() } = {}) {
  const explicit = configuredRoots !== undefined;
  let workspace = new AbortController();
  if (!explicit) {
    server.setNotificationHandler(RootsListChangedNotificationSchema, () => {
      workspace.abort(new Error("The MCP workspace changed. Reconcile the original upload before retrying."));
      workspace = new AbortController();
    });
  }
  return async (signal) => {
    if (explicit) return { roots: parseUploadRoots(configuredRoots), signal };
    const scopeSignal = AbortSignal.any([signal, workspace.signal].filter(Boolean));
    scopeSignal.throwIfAborted();
    let roots;
    if (server.getClientCapabilities()?.roots) {
      const result = await server.listRoots(undefined, { signal: scopeSignal, timeout: 5000 });
      scopeSignal.throwIfAborted();
      if (result.roots.length > 16) throw new Error("The MCP client may advertise at most 16 upload workspace roots.");
      roots = result.roots.map(({ uri }) => workspaceDirectory(uri));
    } else {
      roots = [cwd];
    }
    roots = parseUploadRoots(JSON.stringify(roots));
    if (!roots.length || roots.some((root) => broadDirectory(root, home))) {
      throw new Error(
        "No bounded upload workspace is available. Open a project folder, start MCP there, or set AIPERMISSION_UPLOAD_ROOTS.",
      );
    }
    return { roots, signal: scopeSignal };
  };
}

function workspaceDirectory(uri) {
  try {
    const url = new URL(uri);
    if (url.protocol !== "file:" || url.search || url.hash) throw new Error("Not a local directory URI.");
    return path.posix.normalize(fileURLToPath(url, { windows: false })).replace(/\/$/, "") || "/";
  } catch {
    throw new Error("MCP upload workspace roots must be local file directory URIs.");
  }
}

function broadDirectory(root, home) {
  return root === "/" || root === home || home.startsWith(`${root}/`);
}
