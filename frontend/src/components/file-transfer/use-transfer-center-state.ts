import { useCallback, useMemo, useRef, useState } from "react";
import { apiGet, apiPost } from "../../lib/api";
import { pollReadOptions } from "../../lib/async-resource";
import { isActiveTransferBatch } from "../app-shell-runtime";
import { errorMessage } from "../../lib/errors";
import { createFileTransferBatchActions } from "./file-transfer-actions";
import { createFileTransferListState, fileTransferListBatchResponse, fileTransferListResponse, loadCurrentFileTransferBatches, type FileTransferListState } from "./file-transfer-list-state";

export function useTransferCenterState({ pollIsCurrent }: { pollIsCurrent: (_generation: number | undefined) => boolean }) {
  const [open, setOpen] = useState(false);
  const [batches, setBatches] = useState<FileTransferListState>({ state: "loading", data: [], error: null });
  const seenPendingApprovalsRef = useRef(new Set<number>());
  const listState = useRef(createFileTransferListState()).current;

  const loadBatches = useCallback(
    async (options: { keepData?: boolean } = {}, generation?: number) =>
      loadCurrentFileTransferBatches({
        request: async () => {
          const response: unknown = await apiGet("/api/file-transfer-batches?limit=30", pollReadOptions(undefined, generation));
          return fileTransferListResponse(response);
        },
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
          setBatches((current) => ({ state: "error", data: options.keepData ? current.data : [], error: errorMessage(error, "Could not load file transfers.") }));
        },
      }),
    [listState, pollIsCurrent],
  );

  const actions = useMemo(
    () =>
      createFileTransferBatchActions({
        post: async (path, body) => {
          const response: unknown = await apiPost(path, body);
          return fileTransferListBatchResponse(response);
        },
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
