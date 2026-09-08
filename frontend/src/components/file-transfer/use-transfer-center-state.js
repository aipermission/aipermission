import { useCallback, useMemo, useRef, useState } from "react";
import { apiGet, apiPost } from "../../lib/api";
import { isActiveTransferBatch } from "../app-shell-runtime";
import { createFileTransferBatchActions } from "./file-transfer-actions";
import { createFileTransferListState, loadCurrentFileTransferBatches } from "./file-transfer-list-state";

export function useTransferCenterState({ pollIsCurrent }) {
  const [open, setOpen] = useState(false);
  const [batches, setBatches] = useState({ state: "loading", data: [], error: null });
  const seenPendingApprovalsRef = useRef(new Set());
  const listState = useRef(createFileTransferListState()).current;

  const loadBatches = useCallback(
    async (options = {}, generation) =>
      loadCurrentFileTransferBatches({
        request: () => apiGet("/api/file-transfer-batches?limit=30"),
        pollGeneration: generation,
        pollIsCurrent,
        listState,
        onItems: (items) => {
          const pendingApprovals = items.filter((item) => item.status === "pending_approval");
          const hasNewPendingApproval = pendingApprovals.some((item) => !seenPendingApprovalsRef.current.has(item.id));
          pendingApprovals.forEach((item) => seenPendingApprovalsRef.current.add(item.id));
          if (hasNewPendingApproval) setOpen(true);
          setBatches({ state: "ready", data: items, error: null });
        },
        onError: (error) => {
          setBatches((current) => ({ state: "error", data: options.keepData ? current.data : [], error: error.message }));
        },
      }),
    [listState, pollIsCurrent],
  );

  const actions = useMemo(
    () =>
      createFileTransferBatchActions({
        post: apiPost,
        applyResult: (batch) => setBatches((current) => listState.applyBatch(current, batch)),
        refresh: loadBatches,
      }),
    [listState, loadBatches],
  );

  return {
    actions,
    activeCount: batches.data.filter(isActiveTransferBatch).length,
    batches,
    close: () => setOpen(false),
    loadBatches,
    open,
    show: () => setOpen(true),
  };
}
