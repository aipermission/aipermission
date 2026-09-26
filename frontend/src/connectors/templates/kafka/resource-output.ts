import type { KafkaDetail, KafkaPartition, KafkaResource } from "./console-types";

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

export function kafkaOutputResources(output: unknown, field: "topics" | "consumer_groups"): KafkaResource[] {
  if (!isRecord(output) || !Array.isArray(output[field])) return [];
  return output[field].filter((row): row is KafkaResource => {
    if (!isRecord(row) || typeof row.name !== "string") return false;
    return (
      ["state", "protocol_type"].every((key) => row[key] === undefined || typeof row[key] === "string") &&
      ["partition_count", "replication_factor"].every(
        (key) => row[key] === undefined || (typeof row[key] === "number" && Number.isFinite(row[key])),
      )
    );
  });
}

export function kafkaOutputDetail(output: unknown): KafkaDetail | null {
  if (!isRecord(output)) return null;
  if (output.members !== undefined && !Array.isArray(output.members)) return null;
  if (output.partitions !== undefined && (!Array.isArray(output.partitions) || !output.partitions.every(isPartition))) return null;
  return output;
}

function isPartition(value: unknown): value is KafkaPartition {
  if (!isRecord(value) || !Number.isInteger(value.partition) || typeof value.partition !== "number" || value.partition < 0) return false;
  return ["topic", "error", "committed_offset", "end_offset", "earliest_offset"].every(
    (key) => value[key] === undefined || typeof value[key] === "string",
  );
}
