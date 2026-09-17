import assert from "node:assert/strict";
import test from "node:test";
import { checkForUpdates, compareVersions } from "./update-check.js";

function release(version) {
  return {
    tag_name: `v${version}`,
    html_url: `https://github.com/aipermission/aipermission/releases/tag/v${version}`,
  };
}

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
    assert.equal(compareVersions(ordered[index], ordered[index - 1]), 1);
  }
  assert.equal(compareVersions("1.0.0-rc.10", "1.0.0-rc.2"), 1);
  assert.equal(compareVersions("1.0.0+build.2", "1.0.0+build.1"), 0);
  assert.equal(compareVersions("v2.0", "1.999.999"), 1);
});

test("checkForUpdates compares stable and prerelease responses", async (t) => {
  let remoteVersion = "0.1.2";
  t.mock.method(globalThis, "fetch", async () => ({
    ok: true,
    json: async () => release(remoteVersion),
  }));
  let result = await checkForUpdates("0.1.1");
  assert.deepEqual([result.latestVersion, result.localVersion, result.updateAvailable], ["0.1.2", "0.1.1", true]);
  remoteVersion = "0.1.1-rc.1";
  result = await checkForUpdates("0.1.1");
  assert.equal(result.updateAvailable, false);
});

test("checkForUpdates falls back when the latest stable release is missing", async (t) => {
  const urls = [];
  t.mock.method(globalThis, "fetch", async (url) => {
    urls.push(String(url));
    return String(url).endsWith("/releases/latest") ? { ok: false, status: 404 } : { ok: true, json: async () => [release("0.1.0-rc.1")] };
  });
  const result = await checkForUpdates("0.1.1");
  assert.equal(result.latestVersion, "0.1.0-rc.1");
  assert.equal(result.updateAvailable, false);
  assert.deepEqual(urls, [
    "https://api.github.com/repos/aipermission/aipermission/releases/latest",
    "https://api.github.com/repos/aipermission/aipermission/releases?per_page=1",
  ]);
});
