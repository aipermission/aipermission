import type { components } from "../../../types/generated-openapi";
import { objectRecord } from "../api-types";

export type ConnectorCapacityReport = components["schemas"]["ConnectorCapacityReport"];
export type ConnectorTokenCapacity = ConnectorCapacityReport["items"][number];

function count(value: unknown, positive = false): number {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < (positive ? 1 : 0)) {
    throw new Error("Connector capacity response is invalid.");
  }
  return value;
}

function tokenCapacity(value: unknown, reservation: number): ConnectorTokenCapacity {
  const item = objectRecord(value);
  if (
    !item ||
    typeof item.token_id !== "string" ||
    !/^[1-9][0-9]{0,18}$/.test(item.token_id) ||
    BigInt(item.token_id) > 9223372036854775807n ||
    typeof item.name !== "string" ||
    typeof item.level !== "string" ||
    !["ok", "warning", "critical", "exhausted"].includes(item.level)
  ) {
    throw new Error("Connector capacity response is invalid.");
  }
  const rows = count(item.rows);
  const stored_bytes = count(item.stored_bytes);
  const reserved_bytes = count(item.reserved_bytes);
  const running = count(item.running);
  const pending = count(item.pending);
  if (
    running + pending > rows ||
    reserved_bytes !== (running + pending) * reservation ||
    !Number.isSafeInteger(stored_bytes + reserved_bytes)
  ) {
    throw new Error("Connector capacity response is invalid.");
  }
  return {
    token_id: item.token_id,
    name: item.name,
    rows,
    stored_bytes,
    reserved_bytes,
    running,
    pending,
    level: item.level as ConnectorTokenCapacity["level"],
  };
}

export function connectorCapacityResponse(value: unknown): ConnectorCapacityReport {
  const data = objectRecord(value);
  if (!data || !Array.isArray(data.items)) throw new Error("Connector capacity response is invalid.");
  const next_request_reservation_bytes = count(data.next_request_reservation_bytes, true);
  const items = data.items.map((item) => tokenCapacity(item, next_request_reservation_bytes));
  if (new Set(items.map((item) => item.token_id)).size !== items.length) throw new Error("Connector capacity response is invalid.");
  return {
    items,
    row_limit: count(data.row_limit, true),
    byte_limit: count(data.byte_limit, true),
    running_limit: count(data.running_limit, true),
    next_request_reservation_bytes,
    history_days: count(data.history_days),
  };
}
