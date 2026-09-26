export type BulkCommandItem = {
  request_id: number;
  target_id: number;
  target_name: string;
  status: string;
  stdout?: string;
  stderr?: string;
  error?: string;
  exit_code?: number;
};
export type BulkCommandState = {
  state: "idle" | "starting" | "running" | "done" | "error";
  error: string | null;
  items: BulkCommandItem[];
  parallelism: number;
};

export function bulkCommandResponse(value: unknown): { items: BulkCommandItem[]; parallelism: number } {
  if (
    !objectRecord(value) ||
    !positiveID(value.parallelism) ||
    !Array.isArray(value.items) ||
    !value.items.length ||
    !value.items.every(validBulkItem)
  )
    throw new Error("Invalid bulk command response.");
  const ids = new Set(value.items.map((item) => item.request_id));
  if (ids.size !== value.items.length) throw new Error("Duplicate bulk command request identity.");
  return { items: value.items, parallelism: value.parallelism };
}

export function bulkCommandDetailResponse(value: unknown, original: BulkCommandItem): BulkCommandItem {
  if (!objectRecord(value) || value.id !== original.request_id || typeof value.status !== "string" || !validOutput(value))
    throw new Error("Invalid bulk command detail response.");
  return { ...original, ...value, target_id: original.target_id, request_id: original.request_id, status: value.status };
}

function validBulkItem(value: unknown): value is BulkCommandItem {
  return (
    objectRecord(value) &&
    positiveID(value.request_id) &&
    positiveID(value.target_id) &&
    typeof value.target_name === "string" &&
    typeof value.status === "string" &&
    validOutput(value)
  );
}

function validOutput(value: Record<string, unknown>) {
  return (
    ["target_name", "stdout", "stderr", "error"].every((field) => value[field] === undefined || typeof value[field] === "string") &&
    (value.exit_code === undefined || (typeof value.exit_code === "number" && Number.isSafeInteger(value.exit_code)))
  );
}

function objectRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}
function positiveID(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value > 0;
}
