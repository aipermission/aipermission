import { expect, it } from "vitest";
import { auditEntryFixture } from "../../test/audit-fixtures";
import { auditEntryResponse, auditPageResponse } from "./audit-resource-contract";

it("preserves gateway audit metadata and verifies the requested detail identity", () => {
  const item = auditEntryFixture({ id: 5, target_id: 4, runtime_id: 12, payload_json: '{"reason":"Review"}' });
  expect(auditEntryResponse(item, 5)).toEqual(item);
  expect(() => auditEntryResponse(item, 4)).toThrow("Invalid audit");
});
it.each([{ id: "1" }, { event_version: -1 }, { payload_json: {} }, { target_id: "4" }, { target_name: {} }, { created_at: null }])(
  "rejects malformed audit metadata %j",
  (invalid) => {
    expect(() => auditEntryResponse({ ...auditEntryFixture(), ...invalid })).toThrow("Invalid audit");
  },
);
it("accepts terminal pages with an omitted next offset and rejects nonadvancing pages", () => {
  const page = { items: [auditEntryFixture()], total: 5, limit: 1, offset: 1, next_offset: 2 };
  expect(auditPageResponse(page)).toEqual(page);
  expect(auditPageResponse({ items: [], total: 0, limit: 50, offset: 0 }).items).toEqual([]);
  for (const invalid of [{ next_offset: 1 }, { next_offset: null }, { limit: 0 }, { total: -1 }, { items: {} }]) {
    expect(() => auditPageResponse({ ...page, ...invalid })).toThrow("Invalid audit");
  }
});
