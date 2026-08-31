import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const standardLifecycleKinds = ["docker", "kafka", "kubernetes", "mail", "rabbitmq", "redis", "s3"];

test("standard connector models keep generic target and profile CRUD in the shared lifecycle", () => {
  for (const kind of standardLifecycleKinds) {
    const source = readFileSync(new URL(`../${kind}/model.js`, import.meta.url), "utf8");
    assert.match(source, /createTargetProfileLifecycle/, `${kind} must use the shared target/profile lifecycle`);
    assert.match(source, /connectorCredentialRows/, `${kind} must use the shared credential row contract`);
    assert.doesNotMatch(source, /target-profile-save/, `${kind} must not reimplement target/profile persistence`);
    assert.doesNotMatch(source, /\/api\/connector-targets\/.*\/profiles/, `${kind} must not call generic profile routes directly`);
  }
});
