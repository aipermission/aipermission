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
  .filter((entry) => entry.isDirectory() && !entry.name.startsWith("_"))
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
  const address = server.httpServer?.address();
  assert.ok(address && typeof address !== "string", "vite dev server should expose a TCP address");
  const baseURL = `http://127.0.0.1:${address.port}/`;
  let browser;
  try {
    browser = await chromium.launch();
    const page = await browser.newPage();
    const pageErrors = [];
    page.on("pageerror", (error) => pageErrors.push(error));
    await page.goto(baseURL, { waitUntil: "domcontentloaded" });
    const registryResult = await page.evaluate(async (expectedKinds) => {
      const registry = await import("/src/connectors/templates/registry.jsx");
      const redisTemplate = registry.getConnectorTemplate("redis");
      const redisModel = redisTemplate.model;
      const clickHouseModel = registry.getConnectorModel("clickhouse");
      const rabbitMQModel = registry.getConnectorModel("rabbitmq");
      const valkeyTarget = {
        id: 8,
        connector_kind: "redis",
        name: "cache",
        config: { server_family: "valkey", connection_mode: "direct", host: "127.0.0.1", port: 6379, database: 0 },
        profiles: [{ id: 9, label: "default", kind: "username_password", public: {}, risk_label: "cache access" }],
      };
      return {
        expected: Object.fromEntries(expectedKinds.map((kind) => [kind, Boolean(registry.getConnectorTemplate(kind))])),
        models: Object.fromEntries(expectedKinds.map((kind) => [kind, Boolean(registry.getConnectorModel(kind)?.emptyForm)])),
        metadata: Object.fromEntries(expectedKinds.map((kind) => [kind, registry.getConnectorTemplate(kind)?.metadata?.kind])),
        missing: registry.getConnectorTemplate("__missing_connector__") === null,
        redisValkey: {
          catalogLabel: redisTemplate.metadata.label,
          productLabel: redisModel.connectorProductLabel,
          defaultFamily: redisModel.emptyForm().server_family,
          valkeyLabel: redisModel.serverProductLabel(valkeyTarget),
          formFamily: redisModel.formFromTarget({ target: valkeyTarget, profile: valkeyTarget.profiles[0] }).server_family,
          credentialLabel: redisModel.credentialRows({ targets: [valkeyTarget] })[0].connector_label,
          targetSubtitle: redisModel.targetSubtitle({ target: valkeyTarget }),
          blankValueError: redisModel.validateStringWrite({ key: "smoke:key", value: "" }),
          validValueError: redisModel.validateStringWrite({ key: "smoke:key", value: "ready" }),
        },
        transportDefaults: {
          newTarget: {
            clickhouse: clickHouseModel.emptyForm().tls_mode,
            redis: redisModel.emptyForm().tls_mode,
            rabbitmq: rabbitMQModel.emptyForm().scheme,
          },
          legacyTarget: {
            clickhouse: clickHouseModel.formFromTarget({ target: { name: "legacy", config: {}, profiles: [] } }).tls_mode,
            redis: redisModel.formFromTarget({ target: { name: "legacy", config: {}, profiles: [] } }).tls_mode,
            rabbitmq: rabbitMQModel.formFromTarget({ target: { name: "legacy", config: {}, profiles: [] } }).scheme,
          },
          explicitTarget: {
            clickhouse: clickHouseModel.formFromTarget({
              target: { name: "saved", config: { tls_mode: "verify_full" }, profiles: [] },
            }).tls_mode,
            redis: redisModel.formFromTarget({ target: { name: "saved", config: { tls_mode: "verify_full" }, profiles: [] } }).tls_mode,
            rabbitmq: rabbitMQModel.formFromTarget({ target: { name: "saved", config: { scheme: "https" }, profiles: [] } }).scheme,
          },
        },
      };
    }, connectorTemplateKinds);
    assert.deepEqual(registryResult.expected, Object.fromEntries(connectorTemplateKinds.map((kind) => [kind, true])));
    assert.deepEqual(registryResult.models, Object.fromEntries(connectorTemplateKinds.map((kind) => [kind, true])));
    assert.deepEqual(registryResult.metadata, Object.fromEntries(connectorTemplateKinds.map((kind) => [kind, kind])));
    assert.equal(registryResult.missing, true);
    assert.deepEqual(registryResult.redisValkey, {
      catalogLabel: "Redis / Valkey",
      productLabel: "Redis / Valkey",
      defaultFamily: "redis",
      valkeyLabel: "Valkey",
      formFamily: "valkey",
      credentialLabel: "Valkey",
      targetSubtitle: "Valkey · 127.0.0.1:6379/0 · direct",
      blankValueError: "Value is required.",
      validValueError: "",
    });
    assert.deepEqual(registryResult.transportDefaults, {
      newTarget: { clickhouse: "auto", redis: "auto", rabbitmq: "auto" },
      legacyTarget: { clickhouse: "disable", redis: "disable", rabbitmq: "http" },
      explicitTarget: { clickhouse: "verify_full", redis: "verify_full", rabbitmq: "https" },
    });
    assert.deepEqual(
      pageErrors.map((error) => error.message),
      [],
    );
  } finally {
    if (browser) {
      await browser.close();
    }
    await server.close();
  }
});
