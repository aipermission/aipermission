import assert from "node:assert/strict";
import test from "node:test";
import { checkForUpdates, compareVersions } from "./update-check.js";

test("compareVersions follows SemVer prerelease precedence", () => {
  const ordered = [
    "1.0.0-alpha",
    "1.0.0-alpha.1",
    "1.0.0-alpha.beta",
    "1.0.0-beta",
    "1.0.0-beta.2",
    "1.0.0-beta.11",
    "1.0.0-rc.1",
    "1.0.0",
  ];
  for (let index = 1; index < ordered.length; index += 1) {
    assert.equal(compareVersions(ordered[index], ordered[index - 1]), 1, `${ordered[index]} should follow ${ordered[index - 1]}`);
  }
  assert.equal(compareVersions("1.0.0-rc.10", "1.0.0-rc.2"), 1);
  assert.equal(compareVersions("1.0.0+build.2", "1.0.0+build.1"), 0);
  assert.equal(compareVersions("v2.0", "1.999.999"), 1);
});

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

test("checkForUpdates falls back to release list when latest stable release is missing", async () => {
  const originalFetch = globalThis.fetch;
  const urls = [];
  globalThis.fetch = async (url) => {
    urls.push(String(url));
    if (String(url).endsWith("/releases/latest")) {
      return {
        ok: false,
        status: 404,
      };
    }
    return {
      ok: true,
      json: async () => [
        {
          tag_name: "v0.1.0-rc.1",
          html_url: "https://github.com/aipermission/aipermission/releases/tag/v0.1.0-rc.1",
        },
      ],
    };
  };
  try {
    const result = await checkForUpdates("0.1.1");
    assert.equal(result.latestVersion, "0.1.0-rc.1");
    assert.equal(result.updateAvailable, false);
    assert.deepEqual(urls, [
      "https://api.github.com/repos/aipermission/aipermission/releases/latest",
      "https://api.github.com/repos/aipermission/aipermission/releases?per_page=1",
    ]);
  } finally {
    globalThis.fetch = originalFetch;
  }
});
