import { describe, expect, it } from "vitest";
import { vaultCapabilitiesFromDraft, vaultCapabilityDraftFromItems, vaultCapabilityKey } from "../vault-capabilities";

describe("Vault capability drafts", () => {
  const definitions = [{ name: "vault.session_apply", allowed_rules: ["approval_required", "always_run"] }];

  it("preserves project identity, rule, and temporary expiry through a round trip", () => {
    const items = [
      { project_id: 7, capability_name: "vault.session_apply", execution_rule: "always_run", expires_at: "2026-10-01T12:00:00Z" },
      { project_id: 8, capability_name: "vault.session_apply", execution_rule: "approval_required" },
    ];
    const draft = vaultCapabilityDraftFromItems(items, definitions);
    expect(draft[vaultCapabilityKey(7, "vault.session_apply")]).toEqual({
      execution_rule: "always_run",
      expires_at: "2026-10-01T12:00:00Z",
    });
    expect(draft[vaultCapabilityKey(8, "vault.session_apply")]).toEqual({ execution_rule: "approval_required", expires_at: "" });
    expect(vaultCapabilitiesFromDraft([{ project_id: 7 }, { project_id: 8 }], definitions, draft)).toEqual([
      items[0],
      { ...items[1], expires_at: undefined },
    ]);
  });

  it("does not load unknown or unsupported rules into a draft", () => {
    const items = [
      { project_id: 7, capability_name: "unknown", execution_rule: "always_run" },
      { project_id: 7, capability_name: "vault.session_apply", execution_rule: "blocked" },
      { project_id: 7, capability_name: "without_rules", execution_rule: "always_run" },
    ];
    expect(vaultCapabilityDraftFromItems(items, [...definitions, { name: "without_rules" }])).toEqual({});
    expect(vaultCapabilityDraftFromItems(items, undefined)).toEqual({});
  });

  it("handles missing lists without producing capabilities", () => {
    expect(vaultCapabilityDraftFromItems(null, definitions)).toEqual({});
    expect(vaultCapabilityDraftFromItems(undefined, null)).toEqual({});
    expect(vaultCapabilitiesFromDraft(null, definitions, {})).toEqual([]);
    expect(vaultCapabilitiesFromDraft(undefined, undefined, {})).toEqual([]);
    expect(vaultCapabilitiesFromDraft([{ project_id: 7 }], null, {})).toEqual([]);
  });

  it("omits unselected and disabled drafts and ignores other projects", () => {
    expect(
      vaultCapabilitiesFromDraft([{ project_id: 7 }, { project_id: 8 }], definitions, {
        [vaultCapabilityKey(7, "vault.session_apply")]: { execution_rule: "", expires_at: "" },
        [vaultCapabilityKey(9, "vault.session_apply")]: { execution_rule: "always_run", expires_at: "" },
      }),
    ).toEqual([]);
  });
});
