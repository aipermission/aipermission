import {
  consoleCommandBatch,
  consoleCommandDetail,
  isDefinitiveConsoleCommandStatus,
  isUnknownConsoleCommandStatus,
} from "../../../lib/gateway-contracts/console-command-contract";
import type { CommandBatchItem } from "../../../lib/gateway-contracts/console-command-contract";

export type BulkCommandItem = CommandBatchItem & { refresh_error?: string };
export type BulkCommandState = {
  state: "idle" | "starting" | "running" | "done" | "error";
  error: string | null;
  items: BulkCommandItem[];
  parallelism: number;
  activity_error?: string;
};

export function bulkCommandResponse(value: unknown): { items: BulkCommandItem[]; parallelism: number } {
  const batch = consoleCommandBatch(value);
  return { ...batch, items: batch.items.map((item) => ({ ...item, observed: false })) };
}

export function bulkCommandDetailResponse(value: unknown, original: BulkCommandItem): BulkCommandItem {
  const detail = consoleCommandDetail(value, original.request_id);
  if (detail.runtime_id !== original.target_id) throw new Error("Bulk command detail runtime mismatch.");
  return {
    ...original,
    status: detail.status,
    stdout: detail.stdout,
    stderr: detail.stderr,
    error: detail.error,
    exit_code: detail.exit_code,
    observed: true,
    refresh_error: undefined,
  };
}

export function bulkCommandNeedsObservation(item: BulkCommandItem) {
  return !isUnknownConsoleCommandStatus(item.status) && (!item.observed || !isDefinitiveConsoleCommandStatus(item.status));
}
