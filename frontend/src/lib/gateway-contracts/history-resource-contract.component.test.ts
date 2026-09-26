import { expect, it } from "vitest";
import { historyEntryFixture } from "../../test/history-fixtures";
import { historyEntryResponse, historyLabelsResponse, historyPageResponse, historyTargetsResponse } from "./history-resource-contract";

it("preserves a complete history entry and verifies detail identity", () => {
  const entry = historyEntryFixture({ id: 4, output_json: "{}", labels: [{ id: 2, name: "Review", color: "#ffffff" }] });
  expect(historyEntryResponse(entry, 4)).toEqual(entry);
  expect(() => historyEntryResponse(entry, 5)).toThrow("Invalid history");
});
it.each([
  { id: "1" },
  { status: "future" },
  { bytes_done: -1 },
  { input_json: {} },
  { labels: [{ id: 2, name: "Review" }] },
  { target_id: 0 },
  { approval_required: "true" },
])("rejects malformed history metadata %j", (invalid) => {
  expect(() => historyEntryResponse({ ...historyEntryFixture(), ...invalid })).toThrow("Invalid history");
});
it("validates cursor pages with and without exact totals", () => {
  const page = { items: [historyEntryFixture()], limit: 50, has_more: true, next_cursor: "next" };
  expect(historyPageResponse(page)).toEqual(page);
  expect(historyPageResponse({ ...page, total: 5 }).total).toBe(5);
  expect(() => historyPageResponse({ ...page, next_cursor: undefined })).toThrow("Invalid history");
  expect(() => historyPageResponse({ ...page, total: -1 })).toThrow("Invalid history");
  expect(() => historyPageResponse({ ...page, has_more: false })).toThrow("Invalid history");
});
it("validates labels and history facets independently of live connector availability", () => {
  const target = { ref: "runtime:5", connector_kind: "fixture", runtime_id: 5, target_name: "Former target", last_seen_at: "2026-09-26" };
  expect(historyTargetsResponse({ items: [target] })).toEqual([target]);
  expect(() => historyTargetsResponse({ items: [{ ...target, runtime_id: "5" }] })).toThrow("Invalid history");
  expect(historyLabelsResponse([])).toEqual([]);
  expect(() => historyLabelsResponse(null)).toThrow("Invalid history");
});
