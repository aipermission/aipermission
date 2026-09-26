import { describe, expect, it } from "vitest";
import {
  connectorTargetProfileLifetime,
  currentConnectorTargetProfilePermissions,
  effectiveConnectorTargetProfilePermissions,
  profilesForConnectorTarget,
  selectedConnectorProfile,
  writeStoredConnectorProfileID,
} from "./connector-permissions";

describe("connector profile permissions", () => {
  const profiles = [
    { connector_kind: "example", target_id: 7, profile_id: 1 },
    { connector_kind: "example", target_id: 7, profile_id: 2 },
    { connector_kind: "example", target_id: 8, profile_id: 3 },
  ];

  it("keeps profiles within the selected connector target and token", () => {
    expect(profilesForConnectorTarget(profiles, profiles[0])).toEqual(profiles.slice(0, 2));
    writeStoredConnectorProfileID(profiles[0], 11, 2);
    expect(selectedConnectorProfile(11, profiles[0], profiles.slice(0, 2))).toBe(profiles[1]);
    expect(selectedConnectorProfile(12, profiles[0], profiles.slice(0, 2))).toBe(profiles[0]);
  });

  it("excludes other profiles and expired grants before computing lifetime", () => {
    const now = Date.parse("2026-09-26T12:00:00Z");
    const permissions = [
      { target_id: 7, profile_id: 1, action_name: "read", execution_rule: "always_run" as const },
      {
        target_id: 7,
        profile_id: 1,
        action_name: "write",
        execution_rule: "approval_required" as const,
        expires_at: "2026-09-26T11:00:00Z",
      },
      { target_id: 7, profile_id: 2, action_name: "write", execution_rule: "always_run" as const },
    ];
    expect(currentConnectorTargetProfilePermissions(permissions, profiles[0], 1)).toHaveLength(2);
    expect(effectiveConnectorTargetProfilePermissions(permissions, profiles[0], 1, now)).toEqual([permissions[0]]);
    expect(connectorTargetProfileLifetime(permissions, profiles[0], 2)).toBe(permissions[2]);
  });
});
