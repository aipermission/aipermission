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
const goSum = fs.readFileSync(path.join(root, "backend/go.sum"), "utf8");
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
const sqliteVersion = dbSource.match(
  /expectedSQLiteVersion\s*=\s*"([^"]+)"/,
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
if (inventory.schema_version !== 1) {
  failures.push("native dependency inventory schema_version must be 1");
}
if (
  !/^[0-9a-f]{40}$/.test(sqlcipher.wrapper_commit) ||
  !sqlcipher.wrapper_commit.startsWith(
    sqlcipher.go_module_version.slice(
      sqlcipher.go_module_version.lastIndexOf("-") + 1,
    ),
  )
) {
  failures.push(
    "SQLCipher wrapper commit does not match the pinned pseudo-version",
  );
}
if (
  !goSum.includes(
    `${sqlcipher.go_module} ${sqlcipher.go_module_version} ${sqlcipher.go_module_sum}`,
  ) ||
  !goSum.includes(
    `${sqlcipher.go_module} ${sqlcipher.go_module_version}/go.mod ${sqlcipher.go_mod_sum}`,
  )
) {
  failures.push("SQLCipher module checksums do not match backend/go.sum");
}
if (sqliteVersion !== sqlcipher.embedded_sqlite_version) {
  failures.push(
    `embedded SQLite runtime assertion is ${sqliteVersion || "missing"}; inventory expects ${sqlcipher.embedded_sqlite_version}`,
  );
}
if (
  sqlcipher.crypto_provider !== "openssl-3" ||
  !backendDockerfile.includes("ENV CGO_ENABLED=1") ||
  !backendDockerfile.includes(sqlcipher.container.builder_image) ||
  !backendDockerfile.includes(sqlcipher.container.runtime_image) ||
  !sqlcipher.container.build_packages.every((name) =>
    backendDockerfile.includes(name),
  ) ||
  !sqlcipher.container.runtime_packages.every((name) =>
    backendDockerfile.includes(name),
  )
) {
  failures.push(
    "SQLCipher OpenSSL build/runtime packages are missing from the backend image",
  );
}
if (
  sqlcipher.wrapper_repository !== "SE-I-T-Digital/go-sqlcipher" ||
  sqlcipher.wrapper_branch !== "main" ||
  sqlcipher.upstream_repository !== "sqlcipher/sqlcipher" ||
  !/^v\d+\.\d+\.\d+$/.test(sqlcipher.reviewed_upstream_tag) ||
  sqlcipher.reviewed_upstream_tag.slice(1) !==
    sqlcipher.reviewed_upstream_runtime_version ||
  !/^[0-9a-f]{40}$/.test(sqlcipher.reviewed_upstream_commit) ||
  !/^\d{4}-\d{2}-\d{2}$/.test(sqlcipher.reviewed_upstream_date) ||
  Number.isNaN(
    new Date(`${sqlcipher.reviewed_upstream_date}T00:00:00Z`).getTime(),
  ) ||
  !Number.isInteger(sqlcipher.review_max_age_days) ||
  sqlcipher.review_max_age_days < 1 ||
  sqlcipher.advisory_review?.result !== "no-known-applicable-advisory" ||
  !Array.isArray(sqlcipher.advisory_review?.sources) ||
  sqlcipher.advisory_review.sources.length < 2 ||
  !sqlcipher.advisory_review.sources.every((source) =>
    /^https:\/\/github\.com\/[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+\/security\/advisories$/.test(
      source,
    ),
  )
) {
  failures.push("SQLCipher source and advisory review boundary is incomplete");
}
if (failures.length > 0) {
  console.error("Native dependency inventory check failed:");
  for (const failure of failures) console.error(`- ${failure}`);
  process.exit(1);
}

console.log(
  `Native dependency inventory passed: SQLCipher ${runtimeVersion} via ${moduleVersion}.`,
);
