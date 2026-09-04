export function firstHistoryPage(limit = 50) {
  return {
    limit,
    cursor: null,
    pageIndex: 0,
    cursorStack: [null],
  };
}

export function currentHistoryPage(state) {
  return {
    limit: state.limit,
    cursor: state.cursor,
    pageIndex: state.pageIndex,
    cursorStack: state.cursorStack,
  };
}

export function nextHistoryPage(state) {
  if (!state.nextCursor) return null;
  const cursorStack = state.cursorStack.slice(0, state.pageIndex + 1);
  cursorStack.push(state.nextCursor);
  return {
    limit: state.limit,
    cursor: state.nextCursor,
    pageIndex: state.pageIndex + 1,
    cursorStack,
  };
}

export function previousHistoryPage(state) {
  if (state.pageIndex < 1) return null;
  const pageIndex = state.pageIndex - 1;
  return {
    limit: state.limit,
    cursor: state.cursorStack[pageIndex] || null,
    pageIndex,
    cursorStack: state.cursorStack,
  };
}

export function resolvedHistoryTotal(currentTotal, response, page) {
  if (Number.isInteger(response.total)) return response.total;
  const traversed = page.pageIndex * page.limit + (response.items?.length || 0);
  if (response.has_more) return Math.max(currentTotal, traversed + 1);
  return traversed;
}
