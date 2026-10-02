const { createHash } = require("node:crypto");
const fs = require("node:fs");
const path = require("node:path");

const inventoryPath = "docs/security/dependency-licenses.json";
const npmRoots = ["frontend", "packages/mcp", "scripts"];
const nativePath = "docs/security/native-dependencies.json";
const sourcePaths = [
  ...npmRoots.flatMap((root) => [`${root}/package.json`, `${root}/package-lock.json`]),
  "backend/go.mod",
  "backend/go.sum",
  nativePath,
];
const boundary = {
  purpose: "Reviewed dependency metadata, not a legal compliance determination",
  npm: "Verbatim lockfile license declarations; no SPDX interpretation",
  go: "go.mod requirements plus every go.sum module/version, not a resolved build graph",
  evidence: "Local extracted Go cache file hashes; not authenticated against go.sum",
  native: "Reference to the existing native inventory, not native license classification",
  exclusions: "No installed npm files, OS package licenses, image SBOM, Go standard library, or compatibility evaluation",
};

function sha256(value) {
  return createHash("sha256").update(value).digest("hex");
}

function canonical(value) {
  if (Array.isArray(value)) return value.map(canonical);
  if (value && typeof value === "object")
    return Object.fromEntries(
      Object.keys(value)
        .sort()
        .map((key) => [key, canonical(value[key])]),
    );
  return value;
}

function object(value, label) {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error(`Invalid ${label}`);
  return value;
}

function nonempty(value, label) {
  if (typeof value !== "string" || !value.trim()) throw new Error(`Missing ${label}`);
  return value;
}

function moduleIdentity(module, version) {
  nonempty(module, "Go module path");
  nonempty(version, "Go module version");
  if (
    !/^[A-Za-z0-9._~/-]+$/.test(module) ||
    module.split("/").some((part) => !part || part === "." || part === "..") ||
    !/^v[0-9][A-Za-z0-9.+-]*$/.test(version)
  )
    throw new Error(`Unsupported Go module identity: ${module}@${version}`);
  return `${module}@${version}`;
}

function cacheIdentity(module, version) {
  moduleIdentity(module, version);
  return `${module}@${version}`.replace(/[A-Z]/g, (letter) => `!${letter.toLowerCase()}`);
}

function parseGoSum(source) {
  const modules = new Map();
  // go.sum is a three-column record format, not go.mod syntax.
  for (const [index, line] of source.split(/\r?\n/).entries()) {
    if (!line.trim()) continue;
    const fields = line.trim().split(/\s+/);
    if (fields.length !== 3 || !/^h1:[A-Za-z0-9+/]{43}=$/.test(fields[2])) throw new Error(`Invalid go.sum record on line ${index + 1}`);
    const [module, rawVersion, checksum] = fields;
    const modOnly = rawVersion.endsWith("/go.mod");
    const version = modOnly ? rawVersion.slice(0, -7) : rawVersion;
    const key = moduleIdentity(module, version);
    const record = modules.get(key) || { module, version, checksums: {} };
    const field = modOnly ? "go_mod" : "module";
    if (record.checksums[field]) throw new Error(`Duplicate go.sum record: ${key}/${field}`);
    record.checksums[field] = checksum;
    modules.set(key, record);
  }
  if (!modules.size) throw new Error("Empty go.sum inventory");
  return [...modules.entries()].sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0)).map(([, value]) => value);
}

function npmRecords(lock, source) {
  object(lock, source);
  if (lock.lockfileVersion !== 3) throw new Error(`Unsupported lockfile version: ${source}`);
  const packages = object(lock.packages, `${source} packages`);
  if (!packages[""]) throw new Error(`Missing lockfile root: ${source}`);
  return Object.keys(packages)
    .sort()
    .map((location) => {
      const metadata = object(packages[location], `${source}:${location}`);
      if (location && (!location.startsWith("node_modules/") || location.split("/").some((part) => !part || part === "." || part === "..")))
        throw new Error(`Unsupported lockfile package location: ${location}`);
      const name = metadata.name || (location ? location.split("node_modules/").at(-1) : lock.name);
      for (const field of ["resolved", "integrity"])
        if (metadata[field] !== undefined) nonempty(metadata[field], `npm ${field}: ${source}:${location}`);
      return {
        source,
        location,
        name: nonempty(name, `npm package name: ${source}:${location}`),
        version: nonempty(metadata.version, `npm version: ${source}:${location}`),
        declared_license: nonempty(metadata.license, `npm license: ${source}:${location}`),
        resolved: metadata.resolved || null,
        integrity: metadata.integrity || null,
        metadata_sha256: sha256(JSON.stringify(canonical(metadata))),
      };
    });
}

function readInputs(root) {
  const raw = Object.fromEntries(sourcePaths.map((file) => [file, fs.readFileSync(path.join(root, file))]));
  const native = object(JSON.parse(raw[nativePath]), "native dependency reference");
  if (native.schema_version !== 1 || !native.sqlcipher) throw new Error("Invalid native dependency reference");
  object(native.sqlcipher, "native dependency component");
  for (const directory of npmRoots) object(JSON.parse(raw[`${directory}/package.json`]), `${directory}/package.json`);
  return {
    sources: sourcePaths.map((file) => ({ path: file, sha256: sha256(raw[file]) })),
    npm: npmRoots.flatMap((directory) => npmRecords(JSON.parse(raw[`${directory}/package-lock.json`]), `${directory}/package-lock.json`)),
    sums: parseGoSum(raw["backend/go.sum"].toString("utf8")),
    native: { path: nativePath, sha256: sha256(raw[nativePath]) },
  };
}

function goRecords(sums, goMod) {
  object(goMod, "Go parser output");
  nonempty(goMod.Module?.Path, "Go main module");
  if (!Array.isArray(goMod.Require) || !goMod.Require.length) throw new Error("Missing Go requirements");
  for (const field of ["Replace", "Exclude", "Retract"])
    if (goMod[field] != null && (!Array.isArray(goMod[field]) || goMod[field].length))
      throw new Error(`Unsupported go.mod ${field}; owner must extend inventory support`);
  const requirements = new Map();
  const requiredPaths = new Set();
  for (const requirement of goMod.Require) {
    object(requirement, "Go requirement");
    if (requirement.Indirect !== undefined && typeof requirement.Indirect !== "boolean") throw new Error("Invalid Go indirect flag");
    const key = moduleIdentity(requirement.Path, requirement.Version);
    if (requiredPaths.has(requirement.Path)) throw new Error(`Duplicate Go requirement: ${key}`);
    requiredPaths.add(requirement.Path);
    requirements.set(key, requirement.Indirect ? "indirect" : "direct");
  }
  const records = sums.map((record) => ({
    ...record,
    requirement: requirements.get(moduleIdentity(record.module, record.version)) || "sum-only",
  }));
  for (const key of requirements.keys()) {
    const record = records.find((item) => moduleIdentity(item.module, item.version) === key);
    if (!record?.checksums.module || !record.checksums.go_mod) throw new Error(`Missing required Go checksums: ${key}`);
  }
  return records;
}

module.exports = {
  boundary,
  cacheIdentity,
  canonical,
  goRecords,
  inventoryPath,
  moduleIdentity,
  nonempty,
  npmRecords,
  object,
  parseGoSum,
  readInputs,
  sha256,
};
