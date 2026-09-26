import { expect, it } from "vitest";
import { redactionRulesResponse, securitySettingsResponse } from "./security-settings-contract";

const settings = { reusable_tokens: false, expose_mcp_server_metadata: true, mcp_start_enabled: false, redaction_mode: "basic", revision: "settings-1" };

it("preserves the validated security revision and rejects missing fields", () => {
  expect(securitySettingsResponse(settings)).toBe(settings);
  expect(securitySettingsResponse({ ...settings, redaction_mode: "off" }).redaction_mode).toBe("off");
  for (const key of Object.keys(settings)) {
    const value: Record<string, unknown> = { ...settings };
    delete value[key];
    expect(() => securitySettingsResponse(value)).toThrow("Security settings response is invalid.");
  }
});

it.each([null, [], "settings", { ...settings, revision: " " }, { ...settings, redaction_mode: "unknown" }, { ...settings, reusable_tokens: 1 }])("rejects malformed settings without coercing security controls", (value) => {
  expect(() => securitySettingsResponse(value)).toThrow("Security settings response is invalid.");
});

it("validates rule identity and every editable field while preserving metadata", () => {
  const rule = { id: 7, name: "Internal token", pattern: "internal_[a-z0-9]+", enabled: true, created_at: "opaque" };
  expect(redactionRulesResponse([rule])).toEqual([rule]);
  expect(redactionRulesResponse([rule])[0]).toBe(rule);
  expect(redactionRulesResponse([])).toEqual([]);
  for (const key of ["id", "name", "pattern", "enabled"]) {
    const value: Record<string, unknown> = { ...rule };
    delete value[key];
    expect(() => redactionRulesResponse([value])).toThrow("Redaction rule response is invalid.");
  }
});

it.each([null, {}, [null], [[]], [{ id: 0, name: "rule", pattern: "test", enabled: true }], [{ id: 1, name: "rule", pattern: "test", enabled: 1 }]])("rejects malformed redaction rules", (value) => {
  expect(() => redactionRulesResponse(value)).toThrow(/Redaction rules? response is invalid/);
});
