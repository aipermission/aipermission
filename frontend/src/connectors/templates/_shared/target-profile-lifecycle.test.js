import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import test from "node:test";

import { createTargetProfileLifecycle, usesStandardTargetProfileLifecycle } from "./target-profile-lifecycle.js";

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
    assert.doesNotMatch(source, /target-profile-save/, `${kind} must not reimplement target/profile persistence`);
    assert.doesNotMatch(source, /\/api\/connector-targets\/.*\/profiles/, `${kind} must not call generic profile routes directly`);
  }
});

test("shared lifecycle identity cannot be replaced by lookalike functions", () => {
  const lifecycle = createTargetProfileLifecycle({
    connectorKind: "example",
    connectorLabel: "Example",
    targetPayload: () => ({}),
    profilePayload: () => ({}),
  });
  assert.equal(usesStandardTargetProfileLifecycle(lifecycle), true);
  assert.equal(usesStandardTargetProfileLifecycle({ ...lifecycle, save: async () => {} }), false);
});

test("credential form updates compose from the latest form state", () => {
  const lifecycle = createTargetProfileLifecycle({
    connectorKind: "example",
    connectorLabel: "Example",
    targetPayload: () => ({}),
    profilePayload: () => ({}),
  });
  let state = { form: { first: true, second: true }, auxiliary: "preserved" };
  const props = lifecycle.credentialFormProps({
    targets: [],
    formState: state,
    setFormState(update) {
      state = typeof update === "function" ? update(state) : update;
    },
    formMode: "edit",
    state: { state: "idle" },
    onSubmit() {},
  });

  props.onChange((current) => ({ ...current, first: false }));
  props.onChange((current) => ({ ...current, second: false }));

  assert.deepEqual(state, { form: { first: false, second: false }, auxiliary: "preserved" });
});
