import { describe, expect, it } from "vitest";
import { retentionPurgeResponse, retentionSettingsResponse } from "./retention-settings-contract";

const settings = { history_days: 2, audit_days: 7, console_days: 0, message_days: 3 };

describe("retention response contracts", () => {
  it("preserves valid settings and accepts zero to disable cleanup", () => {
    expect(retentionSettingsResponse(settings)).toBe(settings);
  });

  it.each([null, [], {}, { ...settings, history_days: "2" }, { ...settings, audit_days: -1 }, { ...settings, console_days: 1.5 }, { ...settings, message_days: Infinity }])("rejects invalid settings %j", (value) => {
    expect(() => retentionSettingsResponse(value)).toThrow("Retention settings response is invalid.");
  });

  it("validates the purge count before reporting a successful deletion", () => {
    expect(retentionPurgeResponse({ deleted: 0 })).toEqual({ deleted: 0 });
    expect(retentionPurgeResponse({ deleted: 12 })).toEqual({ deleted: 12 });
  });

  it.each([null, [], {}, { deleted: "4" }, { deleted: -1 }, { deleted: 0.5 }, { deleted: Number.MAX_SAFE_INTEGER + 1 }])("rejects invalid purge response %j", (value) => {
    expect(() => retentionPurgeResponse(value)).toThrow("Retention purge response is invalid.");
  });
});
