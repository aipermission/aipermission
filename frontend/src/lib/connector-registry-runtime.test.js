import assert from "node:assert/strict";
import { readdirSync } from "node:fs";
import { dirname, join } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { chromium } from "@playwright/test";
import { createServer } from "vite";

const currentDir = dirname(fileURLToPath(import.meta.url));
const frontendRoot = join(currentDir, "..", "..");
const connectorTemplatesDir = join(currentDir, "..", "connectors", "templates");
const connectorTemplateKinds = readdirSync(connectorTemplatesDir, { withFileTypes: true })
  .filter((entry) => entry.isDirectory())
  .map((entry) => entry.name)
  .sort();

test("connector template registry evaluates at runtime", async () => {
  const server = await createServer({
    configFile: join(frontendRoot, "vite.config.js"),
    root: frontendRoot,
    logLevel: "silent",
    server: { host: "127.0.0.1", port: 0, strictPort: false },
  });
  await server.listen();
  const baseURL = server.resolvedUrls?.local?.[0];
  assert.ok(baseURL, "vite dev server should expose a local URL");
  let browser;
  try {
    browser = await chromium.launch();
    const page = await browser.newPage();
    const pageErrors = [];
    page.on("pageerror", (error) => pageErrors.push(error));
    await page.goto(baseURL, { waitUntil: "domcontentloaded" });
    const registryResult = await page.evaluate(async (expectedKinds) => {
      const registry = await import("/src/connectors/templates/registry.jsx");
      return {
        expected: Object.fromEntries(expectedKinds.map((kind) => [kind, Boolean(registry.getConnectorTemplate(kind))])),
        models: Object.fromEntries(expectedKinds.map((kind) => [kind, Boolean(registry.getConnectorModel(kind)?.emptyForm)])),
        metadata: Object.fromEntries(expectedKinds.map((kind) => [kind, registry.getConnectorTemplate(kind)?.metadata?.kind])),
        missing: registry.getConnectorTemplate("__missing_connector__") === null,
      };
    }, connectorTemplateKinds);
    assert.deepEqual(registryResult.expected, Object.fromEntries(connectorTemplateKinds.map((kind) => [kind, true])));
    assert.deepEqual(registryResult.models, Object.fromEntries(connectorTemplateKinds.map((kind) => [kind, true])));
    assert.deepEqual(registryResult.metadata, Object.fromEntries(connectorTemplateKinds.map((kind) => [kind, kind])));
    assert.equal(registryResult.missing, true);
    assert.deepEqual(pageErrors.map((error) => error.message), []);
  } finally {
    if (browser) {
      await browser.close();
    }
    await server.close();
  }
});
