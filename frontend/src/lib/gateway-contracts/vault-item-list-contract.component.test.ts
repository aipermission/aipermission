import { describe, expect, it } from "vitest";
import { vaultItemListResponse } from "./vault-item-list-contract.ts";

const item = { id: 1, name: "PROJECT_API_TOKEN", owner_project_id: 4, project_ids: [5], provider: "Provider" };

describe("Vault item list presentation contract", () => {
  it("validates project identity and projects only public selection metadata", () => {
    const response = vaultItemListResponse({ items: [{ ...item, value: "must not reach the selector", value_version: 2 }], total: 1 });
    expect(response.items[0]).toMatchObject(item);
    expect(response.items[0]).not.toHaveProperty("value");
    expect(response.items[0]).not.toHaveProperty("value_version");
    expect(response.total).toBe(1);
  });

  it("normalizes nullable Go slices without inventing an owner identity", () => {
    expect(vaultItemListResponse({ items: [{ ...item, project_ids: null }], total: 1 }).items[0]?.project_ids).toEqual([]);
    expect(vaultItemListResponse({ items: null, total: 0 })).toEqual({ items: [], total: 0 });
  });

  it.each([
    null,
    [],
    {},
    { items: {}, total: 0 },
    { items: [], total: "1" },
    { items: [], total: -1 },
    { items: [], total: 0.5 },
    { items: [{ ...item, id: 0 }], total: 1 },
    { items: [{ ...item, owner_project_id: "4" }], total: 1 },
    { items: [{ ...item, name: {} }], total: 1 },
    { items: [{ ...item, description: [] }], total: 1 },
    { items: [{ ...item, project_ids: [0] }], total: 1 },
  ])("rejects malformed list and identity data (%j)", (value) => {
    expect(() => vaultItemListResponse(value)).toThrow("Invalid Vault item list response.");
  });
});
