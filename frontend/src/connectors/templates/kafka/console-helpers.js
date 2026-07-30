export function connectorActionError(item, fallback = "Kafka action failed.") {
  if (!item || item.status === "failed" || item.status === "canceled" || item.status === "stale") {
    return item?.error || item?.display_text || fallback;
  }
  return "";
}

export function requestIsCurrent(versions, channel, version, targetRef, currentTargetRef) {
  return versions.get(channel) === version && targetRef === currentTargetRef;
}

export function detailMatchesSelection(identity, view, selectedName) {
  return Boolean(selectedName) && identity === `${view}:${selectedName}`;
}
