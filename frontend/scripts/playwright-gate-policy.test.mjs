import assert from "node:assert/strict";
import test from "node:test";

import { requiredHighRiskTitles, responsiveViewportMatrix } from "./playwright-gate-manifest.mjs";
import { assertPlaywrightListing, assertPlaywrightManifestRatchet, forbiddenPlaywrightAnnotations } from "./playwright-gate-policy.mjs";

test("rejects skipped, fixed, and conditionally annotated Playwright tests", () => {
  for (const source of [
    "test.skip('critical', async () => {});",
    "test.fixme('critical', async () => {});",
    "test.fail('critical', async () => {});",
    "test.describe.skip('critical', () => {});",
    "test('critical', async () => { test.skip(condition); });",
    "test['fixme']('critical', async () => {});",
  ]) {
    assert.equal(forbiddenPlaywrightAnnotations(source).length, 1, source);
  }
  assert.deepEqual(forbiddenPlaywrightAnnotations("test('critical', async () => {});"), []);
  assert.deepEqual(forbiddenPlaywrightAnnotations("contest.describe.skip('unrelated', () => {});"), []);
  assert.equal(forbiddenPlaywrightAnnotations(`test.${"describe.".repeat(10_000)}skip('critical', () => {});`).length, 1);
});

test("requires every discovered scenario to be runnable, unannotated, and manifested", () => {
  const report = listing(["one", "two"]);
  assert.doesNotThrow(() => assertPlaywrightListing(report, ["one", "two"], "fixture"));
  assert.throws(() => assertPlaywrightListing(report, ["one", "missing"], "fixture"), /manifest mismatch/);
  report.suites[0].specs[0].tests[0].annotations.push({ type: "skip" });
  assert.throws(() => assertPlaywrightListing(report, ["one", "two"], "fixture"), /has annotations/);
  report.suites[0].specs[0].tests[0].annotations = [];
  report.suites[0].specs[0].tests[0].expectedStatus = "failed";
  assert.throws(() => assertPlaywrightListing(report, ["one", "two"], "fixture"), /has expected status failed/);
});

test("locks the supported responsive matrix and its high-risk scenarios", () => {
  assert.deepEqual(
    responsiveViewportMatrix.map(({ width }) => width),
    [320, 360, 390, 1024, 1280],
  );
  for (const { width, height } of responsiveViewportMatrix) {
    assert.ok(requiredHighRiskTitles.includes(`@high-risk keeps Vault permission completion reachable at ${width}x${height}`));
    assert.ok(
      requiredHighRiskTitles.includes(`@high-risk keeps navigation, Console drawers, and permission dialogs usable at ${width}x${height}`),
    );
  }
});

test("rejects removing a base-branch Playwright gate in the same change", () => {
  const base = { highRisk: ["approval", "unlock"], smoke: ["settings"] };
  assert.doesNotThrow(() => assertPlaywrightManifestRatchet(base, { highRisk: ["approval", "unlock", "new"], smoke: ["settings"] }));
  assert.throws(() => assertPlaywrightManifestRatchet(base, { highRisk: ["approval"], smoke: ["settings"] }), /highRisk: unlock/);
});

for (const [filename, port] of [
  ["playwright.config.js", "4273"],
  ["playwright.real.config.js", "4174"],
]) {
  test(`${filename} uses an isolated strict port without reusing another app`, async () => {
    const previous = process.env.AIPERMISSION_PLAYWRIGHT_PORT;
    process.env.AIPERMISSION_PLAYWRIGHT_PORT = "4273";
    try {
      const { default: config } = await import(`../${filename}?isolated-port-test`);
      const webServer = Array.isArray(config.webServer) ? config.webServer.at(-1) : config.webServer;
      assert.equal(config.use.baseURL, `http://127.0.0.1:${port}`);
      assert.equal(webServer.url, config.use.baseURL);
      assert.equal(webServer.reuseExistingServer, false);
      assert.ok(webServer.command.includes(`--strictPort --port ${port}`));
    } finally {
      if (previous === undefined) delete process.env.AIPERMISSION_PLAYWRIGHT_PORT;
      else process.env.AIPERMISSION_PLAYWRIGHT_PORT = previous;
    }
  });
}

function listing(titles) {
  return {
    suites: [
      {
        specs: titles.map((title) => ({ title, ok: true, tests: [{ annotations: [], expectedStatus: "passed" }] })),
      },
    ],
  };
}
