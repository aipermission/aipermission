import { useCallback, useEffect, useState } from "react";
import { apiGet } from "../../lib/api";
import { useRequestGuard } from "../../lib/request-guard";

const pageSize = 100;
const emptyPage = { key: "", items: [], total: 0, nextOffset: 0, hasMore: false, status: "idle", error: null };

export function useVaultSessionItems({ open, runtimeID, projectID, query }) {
  const guard = useRequestGuard("vault-session-items");
  const key = JSON.stringify([runtimeID, projectID, query.trim()]);
  const [page, setPage] = useState(emptyPage);
  const currentPage = page.key === key ? page : { ...emptyPage, key, status: open && projectID ? "loading" : "idle" };

  const fetchPage = useCallback(
    async (offset, append) => {
      const request = guard.begin("items");
      setPage((current) =>
        append && current.key === key ? { ...current, status: "loading", error: null } : { ...emptyPage, key, status: "loading" },
      );
      try {
        const params = new URLSearchParams({ project_id: String(projectID), limit: String(pageSize), offset: String(offset) });
        if (query.trim()) params.set("q", query.trim());
        const data = await apiGet(`/api/vault-items?${params}`, { signal: request.signal });
        if (!request.isCurrent()) return;
        const incoming = data.items || [];
        const nextOffset = offset + incoming.length;
        setPage((current) => {
          const byID = new Map((append && current.key === key ? current.items : []).map((item) => [Number(item.id), item]));
          incoming.forEach((item) => byID.set(Number(item.id), item));
          return {
            key,
            items: [...byID.values()],
            total: Number(data.total) || 0,
            nextOffset,
            hasMore: incoming.length > 0 && nextOffset < Number(data.total),
            status: "ready",
            error: null,
          };
        });
      } catch (error) {
        if (request.isCurrent()) {
          setPage((current) => ({ ...current, key, status: "error", error: error.message }));
        }
      } finally {
        request.complete();
      }
    },
    [guard, key, projectID, query],
  );

  useEffect(() => {
    guard.invalidate("items");
    if (!open || !projectID) return;
    const timer = window.setTimeout(() => void fetchPage(0, false), query.trim() ? 200 : 0);
    return () => {
      window.clearTimeout(timer);
      guard.invalidate("items");
    };
  }, [fetchPage, guard, open, projectID, query]);

  function loadMore() {
    if (!open || currentPage.status === "loading" || !currentPage.hasMore) return;
    void fetchPage(currentPage.nextOffset, true);
  }

  function retry() {
    if (!open || !projectID || currentPage.status === "loading") return;
    void fetchPage(currentPage.items.length ? currentPage.nextOffset : 0, currentPage.items.length > 0);
  }

  return { ...currentPage, loadMore, retry };
}
