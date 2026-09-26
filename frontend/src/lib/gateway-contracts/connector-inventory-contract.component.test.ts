import { expect, it } from "vitest";
import { inventoryProfileFixture, inventoryTargetFixture } from "../../test/connector-inventory-fixtures";
import { connectorInventoryResponse } from "./connector-inventory-contract";
import { connectorActionsResponse, connectorCatalogDetailResponse, connectorCatalogResponse } from "./connector-catalog-contract";

it("retains target and profile metadata with independent runtime identities", () => {
  const target = inventoryTargetFixture({
    profiles: [inventoryProfileFixture({ runtime_id: 90, transfer_runtime_id: 91 })],
    config: { host: "example.invalid" },
  });
  expect(connectorInventoryResponse({ items: [target] })).toEqual([target]);
});
it.each([
  { id: "3" },
  { profiles: {} },
  { profiles: [inventoryProfileFixture({ target_id: 4 })] },
  { profiles: [inventoryProfileFixture({ connector_kind: "other" })] },
  { profiles: [inventoryProfileFixture(), inventoryProfileFixture()] },
  { config: [] },
])("rejects malformed or cross-target inventory %j", (invalid) => {
  expect(() => connectorInventoryResponse({ items: [{ ...inventoryTargetFixture(), ...invalid }] })).toThrow("Invalid connector inventory");
});
it("validates catalog metadata, unique kinds, and matching detail identity", () => {
  const item = { kind: "example", label: "Example", version: "0.2" };
  expect(connectorCatalogResponse({ items: [item] })).toEqual([item]);
  expect(connectorCatalogDetailResponse({ ...item, help: {} }, "example").help).toEqual({});
  for (const items of [[item, item], [{ ...item, kind: "../example" }], [{ ...item, label: {} }]]) {
    expect(() => connectorCatalogResponse({ items })).toThrow("Invalid connector catalog");
  }
  expect(() => connectorCatalogDetailResponse(item, "other")).toThrow("Invalid connector catalog");
});
it("uses one metadata contract for permission and inventory action lists", () => {
  const actions = [{ name: "inspect", risk: "read", category: "metadata", description: "Inspect state" }];
  expect(connectorActionsResponse({ items: actions })).toEqual(actions);
  const target = inventoryTargetFixture({ profiles: [inventoryProfileFixture({ actions })] });
  expect(connectorInventoryResponse({ items: [target] })[0].profiles?.[0].actions).toEqual(actions);
  expect(() => connectorActionsResponse({ items: [{ name: "inspect", risk: false }] })).toThrow("Invalid connector action");
});
