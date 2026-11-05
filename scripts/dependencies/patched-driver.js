const fs = require("node:fs");
const path = require("node:path");
const { createHash } = require("node:crypto");

const metadataPath = "third_party/go-mssqldb-upstream.json";
const digest = (bytes) => createHash("sha256").update(bytes).digest("hex");

function checkPatchedDriver(root) {
  const manifestStat = fs.lstatSync(path.join(root, metadataPath));
  if (!manifestStat.isFile() || manifestStat.isSymbolicLink())
    throw new Error("Patched driver manifest must be a regular file");
  const metadata = JSON.parse(
    fs.readFileSync(path.join(root, metadataPath), "utf8"),
  );
  if (
    metadata.module !== "github.com/microsoft/go-mssqldb" ||
    !/^v\d+\.\d+\.\d+$/.test(metadata.version) ||
    !/^[a-f0-9]{40}$/.test(metadata.revision)
  )
    throw new Error("Invalid pinned driver identity");
  const patchPath = path.join(root, "third_party/go-mssqldb-close.patch");
  if (
    !fs.lstatSync(patchPath).isFile() ||
    fs.lstatSync(patchPath).isSymbolicLink() ||
    digest(fs.readFileSync(patchPath)) !== metadata.patch_sha256
  )
    throw new Error("Driver patch identity differs from its reviewed manifest");
  const directory = path.join(root, "third_party/go-mssqldb");
  if (
    !fs.lstatSync(directory).isDirectory() ||
    fs.lstatSync(directory).isSymbolicLink()
  )
    throw new Error("Invalid driver source directory");
  if (
    JSON.stringify(Object.keys(metadata.patched_files).sort()) !==
    JSON.stringify([
      "close_lifecycle_test.go",
      "mssql.go",
      "tds.go",
      "token.go",
    ])
  ) {
    throw new Error("Driver patch changed an undeclared upstream file");
  }
  const sums = fs
    .readFileSync(path.join(root, "backend/go.sum"), "utf8")
    .split(/\r?\n/)
    .map((line) => line.trim().split(/\s+/));
  for (const [version, checksum] of [
    [metadata.version, metadata.module_sum],
    [`${metadata.version}/go.mod`, metadata.go_mod_sum],
  ]) {
    if (
      !checksum ||
      !sums.some(
        ([module, value, sum]) =>
          module === metadata.module && value === version && sum === checksum,
      )
    ) {
      throw new Error(
        "Driver snapshot does not match pinned upstream module checksums",
      );
    }
  }
  const expected = { ...metadata.files, ...metadata.patched_files };
  if (
    !expected["go.mod"] ||
    !expected["LICENSE.txt"] ||
    !expected["close_lifecycle_test.go"]
  ) {
    throw new Error(
      "Patched driver lacks module, license or lifecycle regression evidence",
    );
  }
  const actual = [];
  function walk(dir) {
    for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
      const file = path.join(dir, entry.name);
      if (entry.isSymbolicLink())
        throw new Error("Patched driver contains a symlink");
      if (entry.isDirectory()) walk(file);
      else if (entry.isFile())
        actual.push(path.relative(directory, file).split(path.sep).join("/"));
      else throw new Error("Patched driver contains a non-regular source");
    }
  }
  walk(directory);
  if (
    JSON.stringify(actual.sort()) !==
    JSON.stringify(Object.keys(expected).sort())
  ) {
    throw new Error(
      "Patched driver file inventory differs from its reviewed manifest",
    );
  }
  for (const [file, hash] of Object.entries(expected)) {
    if (
      path.posix.normalize(file) !== file ||
      file.startsWith("/") ||
      file.split("/").includes("..") ||
      !/^[a-f0-9]{64}$/.test(hash) ||
      digest(fs.readFileSync(path.join(directory, file))) !== hash
    ) {
      throw new Error(`Patched driver source identity differs: ${file}`);
    }
  }
  return {
    module: metadata.module,
    version: metadata.version,
    path: "../third_party/go-mssqldb",
    manifest: metadataPath,
    sha256: digest(fs.readFileSync(path.join(root, metadataPath))),
  };
}

module.exports = { checkPatchedDriver, metadataPath };
if (require.main === module) {
  checkPatchedDriver(path.resolve(__dirname, "../.."));
  console.log("Pinned SQL Server driver source integrity passed.");
}
