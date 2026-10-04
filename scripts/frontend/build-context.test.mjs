import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { spawnSync } from "node:child_process";
import {
  cpSync,
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const { parse } = createRequire(
  new URL("../../frontend/package.json", import.meta.url),
)("espree");

test("Docker frontend context carries only build inputs, never local secrets or test artifacts", () => {
  const source = fileURLToPath(new URL("../../frontend", import.meta.url));
  const configImports = parse(
    readFileSync(path.join(source, "vite.config.js"), "utf8"),
    {
      ecmaVersion: "latest",
      sourceType: "module",
    },
  )
    .body.filter(
      (entry) =>
        entry.type === "ImportDeclaration" &&
        entry.source.value.startsWith("./"),
    )
    .map((entry) => path.posix.normalize(entry.source.value));
  const required = [
    "package.json",
    "package-lock.json",
    "index.html",
    "vite.config.js",
    "nginx.conf",
    ...configImports,
    "src/main.jsx",
    "public/robots.txt",
  ];
  const excluded = [
    ".env",
    ".env.production.local",
    "src/.env.local",
    "public/.env.production",
    "src/example.component.test.tsx",
    "src/example.spec.ts",
    "e2e/app.spec.js",
    "test-results/report.html",
    "playwright-report/index.html",
    "node_modules/secret/value",
    "dist/index.html",
    "scripts/unrelated.mjs",
    "npm-debug.log",
    "local-database.aipdb",
  ];
  const temp = mkdtempSync(
    path.join(os.tmpdir(), "aipermission-build-context-"),
  );
  const context = path.join(temp, "context");
  const output = path.join(temp, "output");
  try {
    mkdirSync(context);
    cpSync(
      path.join(source, ".dockerignore"),
      path.join(context, ".dockerignore"),
    );
    for (const name of [...required, ...excluded]) {
      const file = path.join(context, name);
      mkdirSync(path.dirname(file), { recursive: true });
      writeFileSync(file, `fixture:${name}`);
    }
    const result = spawnSync(
      "docker",
      [
        "build",
        "--network=none",
        "--output",
        `type=local,dest=${output}`,
        "-f",
        "-",
        context,
      ],
      {
        input: "FROM scratch\nCOPY . /\n",
        encoding: "utf8",
        timeout: 60000,
        env: { ...process.env, DOCKER_BUILDKIT: "1" },
      },
    );
    assert.equal(result.status, 0, result.error?.message || result.stderr);
    for (const name of required)
      assert.equal(
        readFileSync(path.join(output, name), "utf8"),
        `fixture:${name}`,
      );
    for (const name of excluded)
      assert.equal(
        existsSync(path.join(output, name)),
        false,
        `${name} entered the build context`,
      );
  } finally {
    rmSync(temp, { recursive: true, force: true });
  }
});
