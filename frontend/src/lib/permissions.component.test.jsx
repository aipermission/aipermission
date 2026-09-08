import { describe, expect, it } from "vitest";
import {
  effectiveRule,
  expiresAtFromLifetime,
  maskedToken,
  normalizePermission,
  permissionCardClass,
  permissionExpired,
  permissionLifetimeLabel,
  ruleDotClass,
  ruleLabel,
} from "./permissions";

describe("permission helpers", () => {
  const now = new Date("2026-06-07T11:00:00Z").getTime();

  it("fails closed for malformed and expired grants", () => {
    const malformed = { execution_rule: "always_run", expires_at: "not-a-timestamp" };
    const expired = { execution_rule: "always_run", expires_at: "2026-06-07T10:00:00Z" };

    expect(permissionExpired(malformed, now)).toBe(true);
    expect(effectiveRule(malformed, now)).toBe("");
    expect(permissionLifetimeLabel(malformed, now)).toBe("Invalid expiry");
    expect(permissionExpired(expired, now)).toBe(true);
    expect(effectiveRule(expired, now)).toBe("");
    expect(permissionLifetimeLabel(expired, now)).toBe("Expired");
  });

  it("normalizes legacy grants and calculates deterministic lifetimes", () => {
    expect(normalizePermission(null)).toBeNull();
    expect(normalizePermission("approval_required")).toEqual({ execution_rule: "approval_required", expires_at: "" });
    expect(normalizePermission({})).toEqual({ execution_rule: "", expires_at: "" });
    expect(permissionExpired("approval_required", now)).toBe(false);
    expect(effectiveRule("approval_required", now)).toBe("approval_required");
    expect(permissionLifetimeLabel("approval_required", now)).toBe("Permanent");
    expect(expiresAtFromLifetime("permanent", now)).toBe("");
    expect(expiresAtFromLifetime("unknown", now)).toBe("");
    expect(expiresAtFromLifetime("1h", now)).toBe("2026-06-07T12:00:00.000Z");
  });

  it("formats remaining time across minute, hour, and day boundaries", () => {
    expect(permissionLifetimeLabel({ expires_at: "2026-06-07T11:00:01Z" }, now)).toBe("1m left");
    expect(permissionLifetimeLabel({ expires_at: "2026-06-07T12:00:01Z" }, now)).toBe("1h left");
    expect(permissionLifetimeLabel({ expires_at: "2026-06-08T11:00:01Z" }, now)).toBe("1d left");
  });

  it("maps rules to stable labels and presentation classes", () => {
    expect(["always_run", "approval_required", "blocked", ""].every((rule) => typeof ruleLabel(rule) === "string")).toBe(true);
    expect(ruleDotClass("always_run")).toContain("emerald");
    expect(ruleDotClass("approval_required")).toContain("amber");
    expect(ruleDotClass("blocked")).toContain("red");
    expect(ruleDotClass("")).toContain("red");
    expect(permissionCardClass("always_run")).toContain("good");
    expect(permissionCardClass("approval_required")).toContain("warn");
    expect(permissionCardClass("blocked")).toContain("bad");
    expect(permissionCardClass("")).toContain("bad");
    expect(maskedToken("aip_1234567890abcdef")).toBe("aip_1234...abcdef");
    expect(maskedToken("short")).toBe("short");
    expect(maskedToken("")).toBe("token value unavailable");
  });
});
