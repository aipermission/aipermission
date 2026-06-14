import assert from "node:assert/strict";
import { dirname, join } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { chromium } from "@playwright/test";
import { createServer } from "vite";

const currentDir = dirname(fileURLToPath(import.meta.url));
const frontendRoot = join(currentDir, "..", "..");

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
    const registryResult = await page.evaluate(async () => {
      const registry = await import("/src/connectors/templates/registry.jsx");
      return {
        ssh: Boolean(registry.getConnectorTemplate("ssh")),
        postgres: Boolean(registry.getConnectorTemplate("postgres")),
        redis: registry.getConnectorTemplate("redis") === null,
      };
    });
    assert.deepEqual(registryResult, {
      ssh: true,
      postgres: true,
      redis: true,
    });
    assert.deepEqual(pageErrors.map((error) => error.message), []);
  } finally {
    if (browser) {
      await browser.close();
    }
    await server.close();
  }
});
