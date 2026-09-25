type Partition = {
  topic?: string;
  partition?: number;
  error?: string;
  committed_offset?: unknown;
  end_offset?: unknown;
};

export function detailMatchesSelection(identity: string, view: string, name: string) {
  return Boolean(name) && identity === `${view}:${name}`;
}

export function offsetSelectionValue(partition: Partition | null | undefined) {
  return JSON.stringify([String(partition?.topic || ""), Number(partition?.partition || 0)]);
}

export function parseOffsetSelection(value: string) {
  try {
    const parsed: unknown = JSON.parse(value);
    if (!Array.isArray(parsed) || parsed.length !== 2) return null;
    const [topic, partition] = parsed;
    if (typeof topic !== "string" || !topic || typeof partition !== "number" || !Number.isInteger(partition) || partition < 0) return null;
    return { topic, partition };
  } catch {
    return null;
  }
}

export function actionableOffsetPartitions(partitions: Array<Partition | null> = []): Partition[] {
  return partitions.filter((partition): partition is Partition => {
    if (!partition || partition.error || !partition.topic) return false;
    const partitionID = Number(partition.partition);
    return Number.isInteger(partitionID) && partitionID >= 0 && partition.committed_offset != null && partition.end_offset != null;
  });
}
