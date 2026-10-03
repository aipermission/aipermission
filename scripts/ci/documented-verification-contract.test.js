const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");

const root = path.resolve(__dirname, "../..");
const read = (file) => fs.readFileSync(path.join(root, file), "utf8");
const prose = (value) => value.replace(/\s+/g, " ");

test("backup provider documentation matches the implemented service protocol", () => {
  const client = read("backend/internal/backups/service_client.go");
  const guide = read("docs/providers/aipermission-backup.md");
  const match = client.match(/ServiceProtocol\s+=\s+"(\d+)"/);
  assert.ok(match, "backup service protocol constant is missing");
  assert.match(guide, new RegExp(`backup service protocol v${match[1]}\\b`));
});

test("MCP content-boundary and browser retry documentation match runtime contracts", () => {
  const contentBoundaryGuides = [
    "packages/mcp/README.md",
    "docs/api/mcp-tools.md",
    "docs/api/rest-api.md",
    "docs/skills/aipermission-operator/SKILL.md",
    "packages/mcp/resources/aipermission-operator/SKILL.md",
  ].map((file) => prose(read(file)));
  for (const guide of contentBoundaryGuides) {
    assert.match(
      guide,
      /File-transfer queue and status responses do not include transferred file bytes/,
    );
    assert.match(
      guide,
      /Explicitly authorized connector read actions may return bounded content/,
    );
    assert.doesNotMatch(
      guide,
      /(?:MCP )?connector responses never include file contents/i,
    );
  }

  const connectorGuide = prose(read("docs/development/add-a-connector.md"));
  assert.match(connectorGuide, /HMAC-SHA-256/);
  assert.match(connectorGuide, /non-extractable, origin-local signing key/);
  assert.match(connectorGuide, /bounded IndexedDB ledger/);
  assert.match(
    connectorGuide,
    /raw action input and credential values do not enter browser storage/,
  );
});
