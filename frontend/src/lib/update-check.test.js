import assert from "node:assert/strict";
import test from "node:test";
import { checkForUpdates } from "./update-check.js";

test("checkForUpdates reports newer stable releases", async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async () => ({
    ok: true,
    json: async () => ({
      tag_name: "v0.1.2",
      html_url: "https://github.com/aipermission/aipermission/releases/tag/v0.1.2",
    }),
  });
  try {
    const result = await checkForUpdates("0.1.1");
    assert.equal(result.latestVersion, "0.1.2");
    assert.equal(result.localVersion, "0.1.1");
    assert.equal(result.updateAvailable, true);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("checkForUpdates treats prereleases as older than stable releases", async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async () => ({
    ok: true,
    json: async () => ({
      tag_name: "v0.1.1-rc.1",
      html_url: "https://github.com/aipermission/aipermission/releases/tag/v0.1.1-rc.1",
    }),
  });
  try {
    const result = await checkForUpdates("0.1.1");
    assert.equal(result.updateAvailable, false);
  } finally {
    globalThis.fetch = originalFetch;
  }
});
