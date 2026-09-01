import fs from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const packageDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const repoRoot = path.resolve(packageDir, "..", "..");
const distDir = path.join(packageDir, "dist");

await fs.rm(distDir, { recursive: true, force: true });
await fs.mkdir(path.join(distDir, "resources"), { recursive: true });

for (const entry of await fs.readdir(path.join(packageDir, "src"))) {
  if (entry.endsWith(".js")) {
    await fs.copyFile(path.join(packageDir, "src", entry), path.join(distDir, entry));
  }
}

await fs.cp(path.join(packageDir, "resources"), path.join(distDir, "resources"), {
  recursive: true,
  force: true,
});
await fs.rm(path.join(distDir, "resources", "aipermission-operator"), {
  recursive: true,
  force: true,
});
await fs.cp(path.join(repoRoot, "docs", "skills", "aipermission-operator"), path.join(distDir, "resources", "aipermission-operator"), {
  recursive: true,
  force: true,
});

for (const entry of ["cli.js", "server.js"]) {
  await fs.chmod(path.join(distDir, entry), 0o755);
}
