const fs = require("node:fs");

const read = (path) => fs.readFileSync(path, "utf8");
const requireMatch = (path, pattern, label) => {
  const match = read(path).match(pattern);
  if (!match) {
    throw new Error(`${path} does not declare ${label}`);
  }
  return match[1];
};

const expected = requireMatch("backend/go.mod", /^toolchain go([^\s]+)$/m, "a Go toolchain");
const declarations = [
  ["backend/Dockerfile", /^FROM golang:([^\s-]+)-/m, "a Go builder image"],
  [".github/workflows/ci.yml", /go-version: "([^"]+)"/, "a Go CI version"],
  [".github/workflows/codeql.yml", /go-version: "([^"]+)"/, "a Go CodeQL version"],
];

for (const [path, pattern, label] of declarations) {
  const actual = requireMatch(path, pattern, label);
  if (actual !== expected) {
    throw new Error(`${path} uses Go ${actual}; expected ${expected} from backend/go.mod`);
  }
}

process.stdout.write(`Go toolchain declarations match ${expected}.\n`);
