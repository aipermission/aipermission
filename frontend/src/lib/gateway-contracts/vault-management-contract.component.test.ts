import { expect, it } from "vitest";
import { vaultManagedItemsResponse } from "./vault-management-contract.ts";

const item = { id: 1, name: "DEPLOY_KEY", owner_project_id: 2, source: "imported", secret_type: "api_key", value_version: 3, metadata_revision: 4 };
it("keeps searchable metadata and optimistic revisions without copying secret values", () => {
  const data = vaultManagedItemsResponse({ items: [{ ...item, value: "must-not-copy", tags: null, usage_notes: null, project_ids: null }], total: 1 });
  expect(data.items[0]).toMatchObject({ ...item, tags: [], usage_notes: [], project_ids: [] });
  expect(data.items[0]).not.toHaveProperty("value");
});
it.each([
  { value_version: "3" }, { value_version: 0 }, { metadata_revision: -1 }, { metadata_revision: 1.5 },
  { tags: [1] }, { usage_notes: [{ location: "config", notes: 4 }] }, { source: null }, { secret_type: "" },
  { expiry_warning_days: "14" }, { expiry_warning_days: -1 }, { expires_at: 4 }, { last_used_at: false },
])("rejects invalid management metadata %j", (patch) => {
  expect(() => vaultManagedItemsResponse({ items: [{ ...item, ...patch }], total: 1 })).toThrow();
});
it.each([{}, { items: {}, total: 0 }, { items: [], total: -1 }, { items: [], total: "0" }])("rejects malformed list envelopes %j", (value) => {
  expect(() => vaultManagedItemsResponse(value)).toThrow();
});
it("keeps valid usage notes and bounded warning metadata", () => {
  const data = vaultManagedItemsResponse({ items: [{ ...item, tags: ["deploy"], usage_notes: [{ location: "app.env", notes: "rotation" }], expiry_warning_days: 14 }], total: 1 });
  expect(data.items[0]?.usage_notes).toEqual([{ location: "app.env", notes: "rotation" }]);
});
