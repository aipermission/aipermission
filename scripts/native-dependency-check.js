#!/usr/bin/env node

const fs = require("node:fs");
const path = require("node:path");

const root = path.resolve(__dirname, "..");
const inventory = JSON.parse(
  fs.readFileSync(
    path.join(root, "docs/security/native-dependencies.json"),
    "utf8",
  ),
);
const sqlcipher = inventory.sqlcipher;
const goMod = fs.readFileSync(path.join(root, "backend/go.mod"), "utf8");
const dbSource = fs.readFileSync(
  path.join(root, "backend/internal/db/db.go"),
  "utf8",
);
const backendDockerfile = fs.readFileSync(
  path.join(root, "backend/Dockerfile"),
  "utf8",
);
const modulePattern = new RegExp(
  `^\\s*${sqlcipher.go_module.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}\\s+(v\\S+)`,
  "m",
);
const moduleVersion = goMod.match(modulePattern)?.[1];
const runtimeVersion = dbSource.match(
  /expectedSQLCipherVersion\s*=\s*"([^"]+)"/,
)?.[1];

const failures = [];
if (moduleVersion !== sqlcipher.go_module_version) {
  failures.push(
    `SQLCipher Go module is ${moduleVersion || "missing"}; inventory expects ${sqlcipher.go_module_version}`,
  );
}
if (runtimeVersion !== sqlcipher.runtime_version) {
  failures.push(
    `SQLCipher runtime assertion is ${runtimeVersion || "missing"}; inventory expects ${sqlcipher.runtime_version}`,
  );
}
if (!sqlcipher.go_module_version.endsWith(`-${sqlcipher.wrapper_commit}`)) {
  failures.push("SQLCipher wrapper commit does not match the pinned pseudo-version");
}
if (
  sqlcipher.crypto_provider !== "openssl-3" ||
  !backendDockerfile.includes("libssl-dev") ||
  !backendDockerfile.includes("libssl3")
) {
  failures.push(
    "SQLCipher OpenSSL build/runtime packages are missing from the backend image",
  );
}
if (failures.length > 0) {
  console.error("Native dependency inventory check failed:");
  for (const failure of failures) console.error(`- ${failure}`);
  process.exit(1);
}

console.log(
  `Native dependency inventory passed: SQLCipher ${runtimeVersion} via ${moduleVersion}.`,
);
