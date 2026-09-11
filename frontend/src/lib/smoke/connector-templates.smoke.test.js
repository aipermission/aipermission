import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import test from "node:test";
import {
  backendConnectorRegistrySources,
  backendRegisteredConnectorKinds,
  connectorTemplateCatalogSource,
  connectorTemplateKinds,
  connectorTemplateRegistrySource,
  connectorTemplatesDir,
  sourceDir,
} from "./app-smoke-fixtures.test.fixture.js";

test("connector templates are discovered dynamically", () => {
  assert.match(connectorTemplateRegistrySource, /import\.meta\.glob\("\.\/\*\/index\.jsx"/);
  assert.match(connectorTemplateCatalogSource, /import\.meta\.glob\("\.\/\*\/metadata\.json"/);
  assert.doesNotMatch(connectorTemplateRegistrySource, /from "\.\/(ssh|postgres|redis|rabbitmq|kafka|mail|s3|docker|kubernetes)/);
});

test("frontend and backend connector catalogs stay aligned", () => {
  assert.deepEqual(backendRegisteredConnectorKinds(backendConnectorRegistrySources), connectorTemplateKinds);
  for (const kind of connectorTemplateKinds) {
    const indexSource = readFileSync(join(connectorTemplatesDir, kind, "index.jsx"), "utf8");
    const metadata = JSON.parse(readFileSync(join(connectorTemplatesDir, kind, "metadata.json"), "utf8"));
    assert.match(indexSource, /export default Object\.freeze/);
    assert.equal(metadata.kind, kind);
    assert.ok(metadata.label);
    assert.ok(metadata.version);
  }
});

test("backend connector discovery accepts implicit Go import names", () => {
  assert.deepEqual(
    backendRegisteredConnectorKinds([
      `package catalog
import "github.com/aipermission/aipermission/backend/internal/connectors/mysql"
import (
  "github.com/aipermission/aipermission/backend/internal/connectors/redis/transport"
)
func register() { mysql.New(); transport.New() }`,
    ]),
    ["mysql", "redis"],
  );
});

test("connector templates do not import sibling connector internals", () => {
  for (const kind of connectorTemplateKinds) {
    for (const filename of sourceFiles(join(connectorTemplatesDir, kind))) {
      const source = readFileSync(filename, "utf8");
      for (const sibling of connectorTemplateKinds) {
        if (sibling === kind) continue;
        const siblingImport = new RegExp(`(?:from|import\\()\\s*["'][^"']*\\/${sibling}\\/`);
        assert.doesNotMatch(source, siblingImport, `${kind} imports ${sibling}: ${filename}`);
      }
    }
  }
});

test("shared frontend code does not import connector implementations", () => {
  const roots = [join(sourceDir, "pages"), join(sourceDir, "components"), join(sourceDir, "lib")];
  const connectorKindBranch = new RegExp(
    `(?:connector_kind|connectorKind)\\s*(?:===|!==)\\s*["'](?:${connectorTemplateKinds.join("|")})["']`,
  );
  for (const root of roots) {
    for (const filename of sourceFiles(root)) {
      const source = readFileSync(filename, "utf8");
      assert.doesNotMatch(
        source,
        /connectors\/templates\/(?!_shared|registry|catalog|common)/,
        `shared code imports a connector template: ${filename}`,
      );
      assert.doesNotMatch(source, connectorKindBranch, `shared code branches on a connector kind: ${filename}`);
    }
  }
});

function sourceFiles(root) {
  return readdirSync(root, { withFileTypes: true }).flatMap((entry) => {
    const path = join(root, entry.name);
    if (entry.isDirectory()) return sourceFiles(path);
    return /\.(?:js|jsx)$/.test(entry.name) && !/\.test\.[^.]+$/.test(entry.name) ? [path] : [];
  });
}
