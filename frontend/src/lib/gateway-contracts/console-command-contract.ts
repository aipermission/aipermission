import { connectorActionStatuses } from "./generated-connector-contract.ts";
import { objectRecord } from "../api-types.ts";

export type ConsoleCommandStatus = (typeof connectorActionStatuses)[number] | "untracked";
export type CommandBatchIdentity = { request_id: number; target_id: number; status: ConsoleCommandStatus; observed?: boolean };
export type CommandBatchItem = CommandBatchIdentity & {
  target_name: string;
  stdout?: string;
  stderr?: string;
  error?: string;
  exit_code?: number;
};
export type ConsoleCommandDetail = {
  id: number;
  runtime_id: number;
  status: ConsoleCommandStatus;
  stdout?: string;
  stderr?: string;
  error?: string;
  exit_code?: number;
};
const statuses: ReadonlySet<string> = new Set([...connectorActionStatuses, "untracked"]);
const maximumBatchSize = 25;

export function isConsoleCommandStatus(value: unknown): value is ConsoleCommandStatus {
  return typeof value === "string" && statuses.has(value);
}

export function isUnknownConsoleCommandStatus(value: unknown) {
  return value === "outcome_unknown" || value === "untracked";
}

export function isDefinitiveConsoleCommandStatus(value: unknown) {
  return isConsoleCommandStatus(value) && value !== "running" && value !== "approval_pending" && !isUnknownConsoleCommandStatus(value);
}

export function validCommandBatchIdentities(value: unknown): value is CommandBatchIdentity[] {
  if (!Array.isArray(value) || !value.length || value.length > maximumBatchSize || !value.every(validIdentity)) return false;
  return (
    new Set(value.map((item) => item.request_id)).size === value.length &&
    new Set(value.map((item) => item.target_id)).size === value.length
  );
}

export function consoleCommandBatch(
  value: unknown,
  expectedTargetIDs?: readonly number[],
): { items: CommandBatchItem[]; parallelism: number } {
  const row = objectRecord(value);
  if (
    !row ||
    Object.keys(row).some((key) => key !== "parallelism" && key !== "items") ||
    !positiveID(row.parallelism) ||
    row.parallelism > maximumBatchSize ||
    !validCommandBatchIdentities(row.items) ||
    !row.items.every((item) => typeof objectRecord(item)?.target_name === "string" && validOutput(item))
  )
    throw new Error("Invalid bulk command response.");
  if (
    expectedTargetIDs &&
    (row.items.length !== expectedTargetIDs.length || row.items.some((item) => !expectedTargetIDs.includes(item.target_id)))
  )
    throw new Error("Bulk command target identity mismatch.");
  return { items: row.items as CommandBatchItem[], parallelism: row.parallelism };
}

export function consoleCommandDetail(value: unknown, expectedID?: number): ConsoleCommandDetail {
  const row = objectRecord(value);
  if (
    !row ||
    !positiveID(row.id) ||
    (expectedID !== undefined && row.id !== expectedID) ||
    !positiveID(row.runtime_id) ||
    !isConsoleCommandStatus(row.status) ||
    !validOutput(row)
  )
    throw new Error("Invalid console command detail response.");
  return row as ConsoleCommandDetail;
}

function validIdentity(value: unknown): value is CommandBatchIdentity {
  const item = objectRecord(value);
  return Boolean(
    item &&
    positiveID(item.request_id) &&
    positiveID(item.target_id) &&
    isConsoleCommandStatus(item.status) &&
    (item.observed === undefined || typeof item.observed === "boolean"),
  );
}

function validOutput(value: CommandBatchIdentity | Record<string, unknown>) {
  const row = objectRecord(value);
  return Boolean(
    row &&
    ["stdout", "stderr", "error"].every((field) => row[field] === undefined || typeof row[field] === "string") &&
    (row.exit_code === undefined || (typeof row.exit_code === "number" && Number.isSafeInteger(row.exit_code))),
  );
}

function positiveID(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value > 0;
}
