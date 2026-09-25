export type HistoryPageState = { limit: number; cursor: string | null; pageIndex: number; cursorStack: Array<string | null> };
type HistoryPageWithNext = HistoryPageState & { nextCursor?: string | null };
type HistoryResponse = { total?: number; items?: unknown[]; has_more?: boolean };

export function firstHistoryPage(limit = 50): HistoryPageState {
  return {
    limit,
    cursor: null,
    pageIndex: 0,
    cursorStack: [null],
  };
}

export function currentHistoryPage(state: HistoryPageState): HistoryPageState {
  return {
    limit: state.limit,
    cursor: state.cursor,
    pageIndex: state.pageIndex,
    cursorStack: state.cursorStack,
  };
}

export function nextHistoryPage(state: HistoryPageWithNext): HistoryPageState | null {
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

export function previousHistoryPage(state: HistoryPageState): HistoryPageState | null {
  if (state.pageIndex < 1) return null;
  const pageIndex = state.pageIndex - 1;
  return {
    limit: state.limit,
    cursor: state.cursorStack[pageIndex] || null,
    pageIndex,
    cursorStack: state.cursorStack,
  };
}

export function resolvedHistoryTotal(currentTotal: number, response: HistoryResponse, page: HistoryPageState): number {
  if (typeof response.total === "number" && Number.isInteger(response.total)) return response.total;
  const traversed = page.pageIndex * page.limit + (response.items?.length || 0);
  if (response.has_more) return Math.max(currentTotal, traversed + 1);
  return traversed;
}
