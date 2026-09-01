export function createFileTransferListState() {
  let generation = 0;
  return {
    beginRequest() {
      generation += 1;
      return generation;
    },
    isCurrent(requestGeneration) {
      return requestGeneration === generation;
    },
    applyBatch(current, batch) {
      generation += 1;
      const data = [...current.data];
      const index = data.findIndex((item) => Number(item.id) === Number(batch.id));
      if (index === -1) data.unshift(batch);
      else data[index] = { ...data[index], ...batch };
      return { state: "ready", data, error: null };
    },
  };
}

export async function loadCurrentFileTransferBatches({ request, pollGeneration, pollIsCurrent, listState, onItems, onError }) {
  const listGeneration = listState.beginRequest();
  try {
    const data = await request();
    if (!pollIsCurrent(pollGeneration) || !listState.isCurrent(listGeneration)) return [];
    const items = data?.items || [];
    onItems(items);
    return items;
  } catch (error) {
    if (!pollIsCurrent(pollGeneration) || !listState.isCurrent(listGeneration)) return [];
    onError(error);
    return [];
  }
}
