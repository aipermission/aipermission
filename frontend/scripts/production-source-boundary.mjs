import { existsSync, realpathSync } from "node:fs";
import { extname, isAbsolute, relative, resolve, sep } from "node:path";

const productionExtensions = new Set([".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".mts", ".cts"]);

export function productionSourceViolation(id, { frontendRoot, sourceRoot }) {
  const cleanID = String(id || "").split(/[?#]/, 1)[0];
  if (!cleanID || cleanID.startsWith("\0") || !isAbsolute(cleanID)) return "";
  const candidate = existsSync(cleanID) ? realpathSync(cleanID) : resolve(cleanID);
  if (!productionExtensions.has(extname(candidate))) return "";
  if (pathInside(candidate, resolve(frontendRoot, "node_modules"))) return "";
  if (pathInside(candidate, sourceRoot)) return "";
  return candidate;
}

export function productionSourceBoundary(options) {
  const frontendRoot = resolve(options?.frontendRoot || process.cwd());
  const sourceRoot = resolve(options?.sourceRoot || resolve(frontendRoot, "src"));
  return {
    name: "aipermission-production-source-boundary",
    enforce: "pre",
    transform(_source, id) {
      const violation = productionSourceViolation(id, { frontendRoot, sourceRoot });
      if (violation) this.error(`Executable production module must live under frontend/src: ${violation}`);
      return null;
    },
  };
}

function pathInside(candidate, root) {
  const path = relative(resolve(root), candidate);
  return path === "" || (!path.startsWith(`..${sep}`) && path !== ".." && !isAbsolute(path));
}
