import type { HistoryEntry } from "../lib/gateway-contracts/history-resource-contract";

export function historyEntryFixture(overrides: Partial<HistoryEntry> = {}): HistoryEntry {
  return {
    id: 1,
    source_ref_id: 1,
    source_ref_type: "connector_action_request",
    connector_kind: "fixture",
    activity_type: "action",
    source: "mcp",
    status: "completed",
    target_name: "Test target",
    action_name: "read",
    title: "",
    summary: "",
    labels: [],
    progress_current: 0,
    progress_total: 0,
    bytes_done: 0,
    bytes_total: 0,
    approval_required: false,
    created_at: "2026-09-26T12:00:00Z",
    updated_at: "2026-09-26T12:00:00Z",
    ...overrides,
  };
}
