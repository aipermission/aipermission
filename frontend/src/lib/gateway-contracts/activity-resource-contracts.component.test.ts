import { describe, expect, it } from "vitest";
import { backupFreshnessResponse, runtimeMessagesResponse } from "./activity-resource-contracts.ts";

const message = { id: 1, token_id: 2, direction: "ai_to_user", message: "Note", created_at: "2026-09-26" };

describe("activity resource responses", () => {
  it("preserves validated message routing and display fields", () => {
    const values = [message, { ...message, id: 2, direction: "user_to_ai", runtime_id: 3, session_id: 4, token_name: "Agent", target_name: "Target", consumed_at: "2026-09-26" }, { ...message, id: 3, runtime_id: null, session_id: null, consumed_at: null }];
    expect(runtimeMessagesResponse(values)).toEqual(values);
    expect(runtimeMessagesResponse([])).toEqual([]);
  });

  it.each([null, {}, [null], [{ ...message, id: -1 }], [{ ...message, token_id: "2" }], [{ ...message, direction: "other" }], [{ ...message, message: {} }], [{ ...message, created_at: null }], [{ ...message, runtime_id: "3" }], [{ ...message, session_id: -4 }], [{ ...message, token_name: {} }], [{ ...message, target_name: [] }], [{ ...message, consumed_at: 5 }]])("rejects malformed messages %j", (value) => {
    expect(() => runtimeMessagesResponse(value)).toThrow("Invalid runtime messages response");
  });

  it("preserves freshness warnings and check failures independently", () => {
    const items = [{ provider_id: 1, provider_name: "Backup", remote_newer: true, latest_remote_at: "2026-09-26", latest_remote_id: "one", latest_remote_source: "another-machine", latest_known_id: "old", latest_known_at: "2026-09-25" }];
    const checkErrors = [{ provider_id: 2, provider_name: "Offline", error: "offline" }];
    expect(backupFreshnessResponse({ items, check_errors: checkErrors })).toEqual({ items, checkErrors });
    expect(backupFreshnessResponse({ items: null, check_errors: null })).toEqual({ items: [], checkErrors: [] });
  });

  it.each([null, [], { items: {} }, { items: [null] }, { items: [{ provider_id: 1, latest_remote_at: 3 }] }, { items: [{ provider_id: 1, remote_newer: "yes" }] }, { check_errors: {} }, { check_errors: [null] }, { check_errors: [{ provider_id: "2" }] }, { check_errors: [{ provider_id: 2, provider_name: {} }] }, { check_errors: [{ provider_id: 2, error: [] }] }])("rejects malformed freshness %j", (value) => {
    expect(() => backupFreshnessResponse(value)).toThrow("Invalid backup freshness response");
  });
});
