import { afterEach, describe, expect, it, vi } from "vitest";
import { scopedUICookieName } from "./ui-cookie";
import { currentHistoryPage, firstHistoryPage, nextHistoryPage, previousHistoryPage, resolvedHistoryTotal } from "./history-pagination";

afterEach(() => vi.unstubAllGlobals());

describe("UI cookie boundary", () => {
  it("keeps explicit ports isolated and uses only safe cookie-name characters", () => {
    expect(scopedUICookieName("ui", { port: "3210", protocol: "http:" })).toBe("ui_3210");
    expect(scopedUICookieName("ui", { port: "3212", protocol: "http:" })).toBe("ui_3212");
    expect(scopedUICookieName("ui", { port: "port.with spaces", protocol: "http:" })).toBe("ui_port_with_spaces");
    expect(scopedUICookieName("ui", { port: "", protocol: "https:" })).toBe("ui_443");
    expect(scopedUICookieName("ui", { port: "", protocol: "http:" })).toBe("ui_80");
    expect(scopedUICookieName("ui", { port: "", protocol: "file:" })).toBe("ui");
    expect(scopedUICookieName("ui", null)).toBe("ui");
  });

  it("uses browser location only when available", () => {
    expect(scopedUICookieName("ui")).toBe(scopedUICookieName("ui", window.location));
    vi.stubGlobal("window", undefined);
    expect(scopedUICookieName("ui")).toBe("ui");
  });
});

describe("history cursor navigation boundary", () => {
  it("truncates the abandoned forward stack when navigating a new branch", () => {
    const first = firstHistoryPage(2);
    expect(currentHistoryPage(first)).toEqual(first);
    expect(previousHistoryPage(first)).toBeNull();
    expect(nextHistoryPage(first)).toBeNull();
    const second = nextHistoryPage({ ...first, nextCursor: "cursor-a" });
    expect(second).not.toBeNull();
    if (!second) throw new Error("second page is missing");
    expect(previousHistoryPage(second)).toEqual({ ...first, cursorStack: second.cursorStack });
    const branched = nextHistoryPage({ ...first, cursorStack: [null, "abandoned"], nextCursor: "cursor-b" });
    expect(branched?.cursorStack).toEqual([null, "cursor-b"]);
    expect(first.cursorStack).toEqual([null]);
    expect(previousHistoryPage({ ...second, cursorStack: [] })?.cursor).toBeNull();
  });

  it("uses exact totals when present and estimates only the traversed cursor range otherwise", () => {
    const page = { ...firstHistoryPage(2), pageIndex: 2 };
    expect(resolvedHistoryTotal(100, { total: 0 }, page)).toBe(0);
    expect(resolvedHistoryTotal(100, { total: 9 }, page)).toBe(9);
    expect(resolvedHistoryTotal(100, { items: [1], has_more: false }, page)).toBe(5);
    expect(resolvedHistoryTotal(100, { items: [1], has_more: true }, page)).toBe(100);
    expect(resolvedHistoryTotal(0, { total: 1.5, items: [1], has_more: true }, page)).toBe(6);
    expect(resolvedHistoryTotal(0, {}, page)).toBe(4);
  });
});
