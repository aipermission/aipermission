import { existsSync, readFileSync } from "node:fs";
import { posix, resolve } from "node:path";
import { spawnSync } from "node:child_process";

import { testReachesOwner } from "./async-owner-manifest.mjs";

export function validateBehaviorOwnerManifest(manifest, { frontendRoot, graph, sourceFiles }) {
  const failures = [];
  if (manifest?.version !== 1 || !manifest.owners || typeof manifest.owners !== "object" || Array.isArray(manifest.owners)) {
    return ["protected behavior owner manifest must use version 1 with an owners object"];
  }
  for (const [owner, tests] of Object.entries(manifest.owners)) {
    if (!validManifestPath(owner, false)) {
      failures.push(`protected behavior owner path is invalid: ${owner}`);
      continue;
    }
    const ownerPath = resolve(frontendRoot, owner);
    if (!sourceFiles.has(ownerPath)) {
      failures.push(`protected behavior owner is missing: ${owner}`);
      continue;
    }
    if (!Array.isArray(tests) || tests.length === 0 || new Set(tests).size !== tests.length) {
      failures.push(`protected behavior owner must declare unique replacement-capable tests: ${owner}`);
      continue;
    }
    const validTests = tests.filter((testFile) => validManifestPath(testFile, true));
    for (const testFile of tests) {
      if (!validTests.includes(testFile)) failures.push(`protected behavior test path is invalid: ${testFile}`);
    }
    const existingTests = validTests.filter((testFile) => {
      const testPath = resolve(frontendRoot, testFile);
      return existsSync(testPath) && graph.has(testPath);
    });
    for (const testFile of tests) {
      if (!existingTests.includes(testFile)) failures.push(`protected behavior test is missing: ${testFile}`);
    }
    for (const testFile of existingTests) {
      if (!configuredTestSuiteIncludes(testFile)) {
        failures.push(`protected behavior test is outside the configured test suites: ${testFile}`);
        continue;
      }
      const source = readFileSync(resolve(frontendRoot, testFile), "utf8");
      if (!hasExecutableTestDeclaration(source)) {
        failures.push(`protected behavior test has no executable test declaration: ${testFile}`);
      }
    }
    if (
      existingTests.length > 0 &&
      !existingTests.some((testFile) =>
        testReachesOwner({
          graph,
          ownerPath,
          sourceFiles,
          sourceRoot: resolve(frontendRoot, "src"),
          testPath: resolve(frontendRoot, testFile),
        }),
      )
    ) {
      failures.push(`protected behavior tests do not reach their owner: ${owner}`);
    }
  }
  return failures;
}

function validManifestPath(path, testFile) {
  if (typeof path !== "string" || path.includes("\\") || !path.startsWith("src/") || posix.normalize(path) !== path) {
    return false;
  }
  const isTest = path.includes(".test.") || path.includes(".spec.");
  return testFile ? isTest : !isTest;
}

export function behaviorOwnerManifestWeakening(base, current) {
  if (base?.version !== 1 || current?.version !== 1 || !base.owners || !current.owners) {
    return ["protected behavior owner manifest is invalid"];
  }
  const failures = [];
  for (const [owner, tests] of Object.entries(base.owners)) {
    if (!Object.hasOwn(current.owners, owner)) {
      failures.push(`protected behavior owner was removed: ${owner}`);
      continue;
    }
    const currentTests = new Set(Array.isArray(current.owners[owner]) ? current.owners[owner] : []);
    for (const testFile of Array.isArray(tests) ? tests : []) {
      if (!currentTests.has(testFile)) {
        failures.push(`protected behavior test mapping was removed: ${owner} -> ${testFile}`);
      }
    }
  }
  return failures;
}

function configuredTestSuiteIncludes(path) {
  return /\.component\.test\.(?:js|jsx)$/.test(path) || /\.test\.js$/.test(path);
}

function hasExecutableTestDeclaration(source) {
  return /\b(?:it|test)(?:\.(?:each|only|concurrent))*\s*\(/.test(stripCommentsAndStrings(source));
}

function stripCommentsAndStrings(source) {
  let output = "";
  let mode = "code";
  for (let index = 0; index < source.length; index += 1) {
    const current = source[index];
    const next = source[index + 1];
    if (mode === "code") {
      if (current === "/" && next === "/") {
        mode = "line-comment";
        output += "  ";
        index += 1;
      } else if (current === "/" && next === "*") {
        mode = "block-comment";
        output += "  ";
        index += 1;
      } else if (current === "'" || current === '"' || current === "`") {
        mode = current;
        output += " ";
      } else {
        output += current;
      }
      continue;
    }
    if (mode === "line-comment" && current === "\n") {
      mode = "code";
      output += "\n";
    } else if (mode === "block-comment" && current === "*" && next === "/") {
      mode = "code";
      output += "  ";
      index += 1;
    } else if ((mode === "'" || mode === '"' || mode === "`") && current === "\\") {
      output += "  ";
      index += 1;
    } else if (current === mode) {
      mode = "code";
      output += " ";
    } else {
      output += current === "\n" ? "\n" : " ";
    }
  }
  return output;
}

export function readBehaviorOwnerManifestAt(repositoryRoot, ref) {
  const path = "frontend/protected-behavior-owner-tests.json";
  const listing = spawnSync("git", ["ls-tree", "-r", "--name-only", ref, "--", path], {
    cwd: repositoryRoot,
    encoding: "utf8",
  });
  if (listing.error) throw listing.error;
  if (listing.status !== 0) throw new Error(`Cannot inspect protected behavior owner manifest at ${ref}`);
  if (!listing.stdout.trim()) return null;
  const result = spawnSync("git", ["show", `${ref}:frontend/protected-behavior-owner-tests.json`], {
    cwd: repositoryRoot,
    encoding: "utf8",
  });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(`Cannot read protected behavior owner manifest at ${ref}`);
  try {
    return JSON.parse(result.stdout);
  } catch (error) {
    throw new Error(`Protected behavior owner manifest at ${ref} is invalid JSON: ${error.message}`, { cause: error });
  }
}

export function readBehaviorOwnerManifest(path) {
  return JSON.parse(readFileSync(path, "utf8"));
}
