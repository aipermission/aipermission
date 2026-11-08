import { objectRecord } from "../../lib/api-types";
import type { components } from "../../../types/generated-openapi";

export const idleOptions = [10, 20, 30, 45, 60, 120, 180, 360] as const;
export type Session = components["schemas"]["ConsoleCapacitySession"];
export type Report = components["schemas"]["ConsoleCapacityReport"];
export type Group = "all" | "running" | "connected" | "idle" | "other";
const integer = (value: unknown, minimum = 0): value is number =>
  typeof value === "number" && Number.isSafeInteger(value) && value >= minimum;
const timestamp = (value: unknown): value is string => typeof value === "string" && Number.isFinite(Date.parse(value));

export function capacityReport(value: unknown): Report {
  const data = objectRecord(value);
  if (
    !data ||
    !integer(data.limit, 1) ||
    !integer(data.used) ||
    !integer(data.available) ||
    !integer(data.revision, 1) ||
    data.used + data.available !== data.limit ||
    !timestamp(data.observed_at) ||
    !Array.isArray(data.items) ||
    data.items.length !== data.used
  )
    throw new Error("Invalid console capacity response.");
  const items = data.items.map((value): Session => {
    const item = objectRecord(value);
    if (
      !item ||
      !integer(item.id, 1) ||
      !integer(item.runtime_id, 1) ||
      !integer(item.generation, 1) ||
      !integer(item.clients) ||
      !timestamp(item.last_activity) ||
      typeof item.can_close !== "boolean" ||
      !["target_name", "connector_kind", "name", "owner", "state"].every((key) => typeof item[key] === "string") ||
      (item.can_close && item.state !== "connected")
    )
      throw new Error("Invalid console capacity session.");
    return item as Session;
  });
  if (new Set(items.map((item) => item.id)).size !== items.length) throw new Error("Duplicate console capacity session.");
  return { limit: data.limit, used: data.used, available: data.available, revision: data.revision, observed_at: data.observed_at, items };
}

export function sessionGroup(item: Session, report: Report, minutes: number): Group {
  if (item.state === "running") return "running";
  if (item.state !== "connected") return "other";
  return item.can_close && Date.parse(report.observed_at) - Date.parse(item.last_activity) >= minutes * 60_000 ? "idle" : "connected";
}

export function activityAge(item: Session, report: Report) {
  const minutes = Math.max(0, Math.floor((Date.parse(report.observed_at) - Date.parse(item.last_activity)) / 60_000));
  return minutes < 1 ? "Just now" : minutes < 60 ? `${minutes}m ago` : `${Math.floor(minutes / 60)}h ${minutes % 60}m ago`;
}

export function idleLabel(minutes: number) {
  return minutes < 60 ? `${minutes}m` : `${minutes / 60}h`;
}

export function closeResult(value: unknown) {
  const data = objectRecord(value);
  if (
    !data ||
    !["closed", "skipped", "uncertain"].every((key) => Array.isArray(data[key]) && data[key].every((id: unknown) => integer(id, 1)))
  )
    throw new Error("Cleanup result could not be verified. Refresh capacity before trying again.");
  return data as { closed: number[]; skipped: number[]; uncertain: number[] };
}
