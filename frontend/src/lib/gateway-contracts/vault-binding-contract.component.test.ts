import { expect, it } from "vitest";
import { vaultBindingsResponse, vaultBindingTargetsResponse } from "./vault-binding-contract.ts";
const binding = { id: 1, vault_item_id: 2, source_project_id: 3, target_id: 4, profile_id: 5, binding_revision: 6,
  replace_existing: true, target_name: "Target", profile_label: "main", source_project_name: "My Project", connector_kind: "fixture" };
const target = { id: 4, name: "Target", connector_kind: "fixture", profiles: [{ id: 5, label: "main", vault_session_supported: true }] };

it("preserves binding revisions and checks the selected Vault item identity", () => {
  expect(vaultBindingsResponse({ items: [binding] }, 2)).toEqual([binding]);
  expect(() => vaultBindingsResponse({ items: [binding] }, 3)).toThrow(/identity/);
});
it.each([{ id: "1" }, { binding_revision: 0 }, { source_project_id: -1 }, { replace_existing: "true" }, { profile_label: null }])(
  "rejects malformed binding fields %j", (patch) => {
    expect(() => vaultBindingsResponse({ items: [{ ...binding, ...patch }] }, 2)).toThrow();
  });
it("projects only safe target selection metadata", () => {
  expect(vaultBindingTargetsResponse({ items: [{ ...target, credential: "must-not-copy" }] })).toEqual([target]);
});
it.each([{ id: 0 }, { connector_kind: null }, { profiles: {} }, { profiles: [{ id: 5, label: "main", vault_session_supported: "true" }] }])(
  "rejects malformed Vault target selection fields %j", (patch) => {
    expect(() => vaultBindingTargetsResponse({ items: [{ ...target, ...patch }] })).toThrow();
  });
it("normalizes omitted profiles and empty list collections", () => {
  expect(vaultBindingTargetsResponse({ items: [{ id: 4, name: "Target", connector_kind: "fixture" }] })[0]?.profiles).toEqual([]);
  expect(vaultBindingsResponse({ items: null }, 2)).toEqual([]);
});
it("rejects a successful response without a collection field", () => {
  expect(() => vaultBindingsResponse({}, 2)).toThrow();
  expect(() => vaultBindingTargetsResponse({})).toThrow();
});
