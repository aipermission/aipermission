import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import test from "node:test";

test("standard connector models keep generic target and profile CRUD in the shared lifecycle", () => {
  const templates = readdirSync(new URL("../", import.meta.url), { withFileTypes: true })
    .filter((entry) => entry.isDirectory() && entry.name !== "_shared")
    .map((entry) => entry.name)
    .sort();
  for (const kind of templates) {
    const metadata = JSON.parse(readFileSync(new URL(`../${kind}/metadata.json`, import.meta.url), "utf8"));
    assert.ok(["standard", "custom"].includes(metadata.profile_lifecycle), `${kind} must declare its profile lifecycle`);
    if (metadata.profile_lifecycle === "custom") continue;
    const source = readFileSync(new URL(`../${kind}/model.js`, import.meta.url), "utf8");
    assert.match(source, /createTargetProfileLifecycle/, `${kind} must use the shared target/profile lifecycle`);
    assert.match(source, /connectorCredentialRows/, `${kind} must use the shared credential row contract`);
    assert.doesNotMatch(source, /target-profile-save/, `${kind} must not reimplement target/profile persistence`);
    assert.doesNotMatch(source, /\/api\/connector-targets\/.*\/profiles/, `${kind} must not call generic profile routes directly`);
  }
});
