import { describe, expect, it } from "vitest";
import { vaultSessionOptionsResponse } from "./vault-session-options-contract.ts";
import { selectionFromDefaultBinding } from "../vault-session-selection.ts";

const binding = {
  id: 3,
  vault_item_id: 7,
  vault_item_name: "PROJECT_API_KEY",
  source_project_id: 4,
  source_project_name: "My Project",
  replace_existing: true,
  binding_revision: 2,
};
const options = {
  supported: true,
  target_project_id: 4,
  items: [{ id: 7, name: "PROJECT_API_KEY", owner_project_id: 4 }],
  total: 1,
  defaults: [binding],
  projects: [{ id: 4, name: "My Project" }],
};

describe("Vault session option contract", () => {
  it("preserves exact binding identity and revision while dropping unused gateway fields", () => {
    const response = vaultSessionOptionsResponse({
      ...options,
      defaults: [{ ...binding, unused: "not needed" }],
      projects: [{ id: 4, name: "My Project", unused: "not needed" }],
    });
    expect(response.defaults).toEqual([binding]);
    expect(response.projects).toEqual([{ id: 4, name: "My Project" }]);
    expect(selectionFromDefaultBinding(response.defaults[0])).toEqual({
      item_id: 7,
      source_project_id: 4,
      replace_existing: true,
      binding_id: 3,
      binding_revision: 2,
    });
  });

  it("handles an unsupported runtime and nullable empty catalog slices", () => {
    expect(vaultSessionOptionsResponse({ supported: false })).toEqual({ supported: false, items: [], defaults: [], projects: [] });
    expect(vaultSessionOptionsResponse({ ...options, items: null, total: 0, defaults: null, projects: null })).toMatchObject({
      supported: true,
      items: [],
      defaults: [],
      projects: [],
    });
  });

  it.each([
    null,
    [],
    {},
    { ...options, supported: "true" },
    { ...options, target_project_id: 0 },
    { ...options, items: [{}] },
    { ...options, total: "1" },
    { ...options, defaults: [{ ...binding, binding_revision: 0 }] },
    { ...options, defaults: [{ ...binding, replace_existing: "true" }] },
    { ...options, defaults: [{ ...binding, source_project_id: -1 }] },
    { ...options, projects: [{ id: "4", name: "Project" }] },
    { ...options, projects: [{ id: 4, name: {} }] },
  ])("rejects malformed selection context (%j)", (value) => {
    expect(() => vaultSessionOptionsResponse(value)).toThrow(/Invalid Vault/);
  });
});
