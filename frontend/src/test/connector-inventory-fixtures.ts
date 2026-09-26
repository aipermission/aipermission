import type { InventoryProfile, InventoryTarget } from "../lib/gateway-contracts/connector-inventory-contract";
import type { GatewayTarget } from "../lib/gateway-contracts/core-resource-contracts";

export function gatewayTargetFixture(overrides: Partial<GatewayTarget> = {}): GatewayTarget {
  return {
    target_id: 3, profile_id: 11, project_id: 7,
    connector_kind: "example", ref: "example:3:11", target_name: "Test target",
    profile_kind: "identity", profile_label: "Default", project_name: "My Project", project_slug: "my-project",
    status: "idle", created_at: "2026-09-26", updated_at: "2026-09-26",
    ...overrides,
  };
}

export function inventoryProfileFixture(overrides: Partial<InventoryProfile> = {}): InventoryProfile {
  return {
    id: 11,
    target_id: 3,
    connector_kind: "ssh",
    kind: "ssh_identity",
    label: "Default",
    ref: "ssh:3:11",
    vault_session_supported: true,
    created_at: "2026-09-26",
    updated_at: "2026-09-26",
    ...overrides,
  };
}
export function inventoryTargetFixture(overrides: Partial<InventoryTarget> = {}): InventoryTarget {
  return {
    id: 3,
    project_id: 7,
    project_name: "My Project",
    project_slug: "my-project",
    connector_kind: "ssh",
    name: "Test host",
    status: "idle",
    created_at: "2026-09-26",
    updated_at: "2026-09-26",
    profiles: [inventoryProfileFixture(), inventoryProfileFixture({ id: 22, ref: "ssh:3:22", label: "Other" })],
    ...overrides,
  };
}
