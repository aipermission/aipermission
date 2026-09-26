import assert from "node:assert/strict";
import { existsSync, readdirSync, readFileSync } from "node:fs";
import test from "node:test";

import {
  createTargetProfileLifecycle,
  firstTargetCredentialForm,
  standardSubmitLabel,
  usernameCredentialStateFromRow,
  usesStandardTargetProfileLifecycle,
} from "./target-profile-lifecycle.ts";
import type { Dispatch, SetStateAction } from "react";

test("shared connector defaults retain target order and form isolation", () => {
  const defaults = { profile_label: "readonly", target_id: "" };
  const formState = firstTargetCredentialForm(
    [
      { id: 1, connector_kind: "redis" },
      { id: 2, connector_kind: "s3" },
    ],
    "s3",
    defaults,
  );
  assert.deepEqual(formState, { form: { profile_label: "readonly", target_id: "2" } });
  formState.form.profile_label = "changed";
  assert.equal(defaults.profile_label, "readonly");
  assert.equal(firstTargetCredentialForm([], "s3", defaults).form.target_id, "");
  assert.equal(standardSubmitLabel({ state: { state: "saving" }, mode: "edit" }), "Saving...");
  assert.equal(standardSubmitLabel({ state: { state: "idle" }, mode: "edit" }), "Save changes");
  assert.equal(standardSubmitLabel({ state: { state: "idle" }, mode: "create" }), "Create connector");
  assert.deepEqual(
    usernameCredentialStateFromRow({
      target_id: 2,
      name: "readonly",
      profile: { public: { username: "reader" }, risk_label: "read" },
    }),
    { form: { target_id: "2", profile_label: "readonly", username: "reader", password: "", risk_label: "read" } },
  );
});

test("standard connector models keep generic target and profile CRUD in the shared lifecycle", () => {
  const templates = readdirSync(new URL("../", import.meta.url), { withFileTypes: true })
    .filter((entry) => entry.isDirectory() && entry.name !== "_shared")
    .map((entry) => entry.name)
    .sort();
  for (const kind of templates) {
    const metadata = JSON.parse(readFileSync(new URL(`../${kind}/metadata.json`, import.meta.url), "utf8"));
    assert.ok(["standard", "custom"].includes(metadata.profile_lifecycle), `${kind} must declare its profile lifecycle`);
    if (metadata.profile_lifecycle === "custom") continue;
    const typedModel = new URL(`../${kind}/model.ts`, import.meta.url);
    const model = existsSync(typedModel) ? typedModel : new URL(`../${kind}/model.js`, import.meta.url);
    const source = readFileSync(model, "utf8");
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
    setFormState(update: Parameters<Dispatch<SetStateAction<typeof state>>>[0]) {
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
