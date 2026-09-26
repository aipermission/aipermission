import { useRef, useState } from "react";
import { apiPost } from "../../lib/api";
import { forgetDownloadPath, rememberDownloadPath, rememberedDownloadPath } from "../../lib/file-transfer-utils";
import { useRequestGuard } from "../../lib/request-guard";
import { errorMessage } from "../../lib/errors";
import { remoteBrowserResponse, type RemoteBrowserState, type RemoteBrowserOptions, type TransferDirection } from "./transfer-contracts";

const emptyBrowserState: RemoteBrowserState = { open: false, purpose: "upload", path: "/", state: "idle", data: null, error: null };

export function useTransferBrowser({ runtimeTarget, defaultRemoteDir, remoteDir, normalizeRemoteDirectoryInput, onUseDirectory }: {
  runtimeTarget: { id: string | number } | null;
  defaultRemoteDir: string;
  remoteDir: string;
  normalizeRemoteDirectoryInput: (_value: string) => string;
  onUseDirectory: (_path: string) => void;
}) {
  const [browser, setBrowser] = useState(emptyBrowserState);
  const browserRef = useRef(browser);
  browserRef.current = browser;
  const requestGuard = useRequestGuard(`transfer-browser:${runtimeTarget?.id || ""}`);

  function resetBrowser() {
    requestGuard.invalidate("browse");
    setBrowser(emptyBrowserState);
  }

  function openBrowser(purpose: TransferDirection) {
    const nextPath =
      purpose === "download"
        ? rememberedDownloadPath(runtimeTarget, defaultRemoteDir, normalizeRemoteDirectoryInput)
        : normalizeRemoteDirectoryInput(remoteDir || defaultRemoteDir);
    setBrowser({ open: true, purpose, path: nextPath, state: "loading", data: null, error: null });
    void loadBrowser(nextPath, purpose, { fallbackToDefault: purpose === "download" });
  }

  async function loadBrowser(pathValue = browser.path, purpose = browser.purpose, options: RemoteBrowserOptions = {}) {
    if (!runtimeTarget) return;
    const nextPath = normalizeRemoteDirectoryInput(pathValue || "/");
    if (options.append && (browserRef.current.state !== "ready" || browserRef.current.data?.path !== nextPath || browserRef.current.purpose !== purpose)) return;
    const request = requestGuard.begin("browse");
    setBrowser((current) => ({ ...current, purpose, path: nextPath, state: options.append ? "loading-more" : "loading", error: null }));
    try {
      const response: unknown = await apiPost(
        "/api/file-transfers/browse",
        {
          runtime_id: Number(runtimeTarget.id),
          path: nextPath,
          ...(options.cursor ? { cursor: options.cursor } : {}),
        },
        { signal: request.signal },
      );
      if (!request.isCurrent()) return;
      const data = remoteBrowserResponse(response);
      if (purpose === "download") {
        rememberDownloadPath(runtimeTarget, data.path || nextPath, normalizeRemoteDirectoryInput);
      }
      setBrowser((current) => ({
        open: true,
        purpose,
        path: data.path || nextPath,
        state: "ready",
        data: options.append ? { ...data, entries: [...(current.data?.entries || []), ...(data.entries || [])] } : data,
        error: null,
      }));
    } catch (error) {
      if (!request.isCurrent()) return;
      if (purpose === "download" && options.fallbackToDefault && nextPath !== defaultRemoteDir) {
        forgetDownloadPath(runtimeTarget);
        void loadBrowser(defaultRemoteDir, purpose, { fallbackToDefault: false });
        return;
      }
      setBrowser((current) => ({ ...current, purpose, path: nextPath, state: options.append ? "ready" : "error", error: errorMessage(error, "Could not browse remote files.") }));
    } finally {
      request.complete();
    }
  }

  function useBrowserDirectory(pathValue = browser.path) {
    onUseDirectory(pathValue);
    resetBrowser();
  }

  return {
    browser,
    openBrowser,
    loadBrowser,
    closeBrowser: resetBrowser,
    setBrowserPath(path: string) {
      setBrowser((current) => ({ ...current, path }));
    },
    useBrowserDirectory,
    resetBrowser,
  };
}
