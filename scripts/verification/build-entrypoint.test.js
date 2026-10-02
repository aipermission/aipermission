const assert = require("node:assert/strict");
const { spawnSync } = require("node:child_process");
const path = require("node:path");
const test = require("node:test");

test("the common build entrypoint compiles backend, frontend and MCP", () => {
  const result = spawnSync("make", ["-f", "Makefile", "-n", "build"], {
    cwd: path.resolve(__dirname, "../.."),
    encoding: "utf8",
    timeout: 10000,
  });
  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stdout, /cd backend && go build \.\/\.\.\./);
  assert.match(result.stdout, /cd frontend && npm run build/);
  assert.match(result.stdout, /cd packages\/mcp && npm run build/);
});
