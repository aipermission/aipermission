import { useEffect, useRef, useState } from "react";
import { apiDownload } from "../../lib/api";
import { errorMessage } from "../../lib/errors";
import type { components } from "../../../types/generated-openapi";

export function useHistoryTransferDownload(item: Pick<components["schemas"]["HistoryEntry"], "id" | "source_ref_id"> | null, fileName: string) {
  const [downloadState, setDownloadState] = useState<{ state: string; error: string | null }>({ state: "idle", error: null });
  const downloadRef = useRef<{ generation: number; controller: AbortController | null }>({ generation: 0, controller: null });

  useEffect(() => {
    downloadRef.current.generation += 1;
    downloadRef.current.controller?.abort();
    downloadRef.current.controller = null;
    setDownloadState({ state: "idle", error: null });
    return () => {
      downloadRef.current.generation += 1;
      downloadRef.current.controller?.abort();
      downloadRef.current.controller = null;
    };
  }, [item?.id]);

  async function downloadTransfer() {
    if (!item) return;
    downloadRef.current.controller?.abort();
    const controller = new AbortController();
    const generation = downloadRef.current.generation + 1;
    downloadRef.current = { generation, controller };
    setDownloadState({ state: "downloading", error: null });
    try {
      await apiDownload(`/api/file-transfers/${item.source_ref_id}/download`, fileName, {
        picker: true,
        requireStreaming: true,
        signal: controller.signal,
      });
      if (downloadRef.current.generation === generation) setDownloadState({ state: "idle", error: null });
    } catch (error) {
      const aborted = error !== null && typeof error === "object" && "name" in error && error.name === "AbortError";
      if (downloadRef.current.generation === generation) {
        setDownloadState(aborted ? { state: "idle", error: null } : { state: "error", error: errorMessage(error, "Download failed.") });
      }
    } finally {
      if (downloadRef.current.generation === generation) downloadRef.current.controller = null;
    }
  }

  return { downloadTransfer, downloadState };
}
