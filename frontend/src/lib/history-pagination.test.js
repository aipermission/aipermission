import assert from "node:assert/strict";
import test from "node:test";

import { currentHistoryPage, firstHistoryPage, nextHistoryPage, previousHistoryPage, resolvedHistoryTotal } from "./history-pagination.js";

test("history cursor navigation retains cursors for previous pages", () => {
  const first = { ...firstHistoryPage(25), nextCursor: "cursor-page-2" };
  const second = nextHistoryPage(first);
  assert.deepEqual(second, {
    limit: 25,
    cursor: "cursor-page-2",
    pageIndex: 1,
    cursorStack: [null, "cursor-page-2"],
  });

  const third = nextHistoryPage({ ...second, nextCursor: "cursor-page-3" });
  assert.deepEqual(previousHistoryPage(third), {
    limit: 25,
    cursor: "cursor-page-2",
    pageIndex: 1,
    cursorStack: [null, "cursor-page-2", "cursor-page-3"],
  });
});

test("history cursor navigation stops at either boundary", () => {
  const first = firstHistoryPage();
  assert.equal(previousHistoryPage(first), null);
  assert.equal(nextHistoryPage({ ...first, nextCursor: null }), null);
  assert.deepEqual(currentHistoryPage(first), first);
});

test("history cursor totals stay coherent when polling changes the result set", () => {
  const first = firstHistoryPage(50);
  assert.equal(resolvedHistoryTotal(50, { items: Array(50), has_more: true }, first), 51);
  assert.equal(resolvedHistoryTotal(80, { items: Array(12), has_more: false }, first), 12);

  const second = { ...first, pageIndex: 1, cursor: "page-2", cursorStack: [null, "page-2"] };
  assert.equal(resolvedHistoryTotal(51, { items: [{}], has_more: false }, second), 51);
  assert.equal(resolvedHistoryTotal(51, { total: 75, items: [], has_more: false }, second), 75);
});
