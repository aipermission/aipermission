export function requestIsCurrent(versions, channel, version, targetRef, currentTargetRef) {
  return versions.get(channel) === version && targetRef === currentTargetRef;
}

export function detailMatchesSelection(identity, view, name) {
  return Boolean(name) && identity === `${view}:${name}`;
}

export function offsetSelectionValue(partition) {
  return JSON.stringify([String(partition?.topic || ""), Number(partition?.partition || 0)]);
}

export function parseOffsetSelection(value) {
  try {
    const [topic, partition] = JSON.parse(value);
    if (!topic || !Number.isInteger(partition) || partition < 0) return null;
    return { topic, partition };
  } catch {
    return null;
  }
}

export function actionableOffsetPartitions(partitions = []) {
  return partitions.filter((partition) => {
    if (!partition || partition.error || !partition.topic) return false;
    const partitionID = Number(partition.partition);
    return Number.isInteger(partitionID)
      && partitionID >= 0
      && partition.committed_offset != null
      && partition.end_offset != null;
  });
}
