import { useEffect, useRef, useState } from "react";
import { apiDownload } from "../../lib/api";

export function useHistoryTransferDownload(item, fileName) {
  const [downloadState, setDownloadState] = useState({ state: "idle", error: null });
  const downloadRef = useRef({ generation: 0, controller: null });

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
      if (downloadRef.current.generation === generation && error?.name !== "AbortError") {
        setDownloadState({ state: "error", error: error.message });
      }
    } finally {
      if (downloadRef.current.generation === generation) downloadRef.current.controller = null;
    }
  }

  return { downloadTransfer, downloadState };
}
