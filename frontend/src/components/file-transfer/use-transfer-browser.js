import { useState } from "react";
import { apiPost } from "../../lib/api";
import { forgetDownloadPath, rememberDownloadPath, rememberedDownloadPath } from "../../lib/file-transfer-utils";
import { useRequestGuard } from "../../lib/request-guard";

const emptyBrowserState = { open: false, purpose: "upload", path: "/", state: "idle", data: null, error: null };

export function useTransferBrowser({ runtimeTarget, defaultRemoteDir, remoteDir, normalizeRemoteDirectoryInput, onUseDirectory }) {
  const [browser, setBrowser] = useState(emptyBrowserState);
  const requestGuard = useRequestGuard(`transfer-browser:${runtimeTarget?.id || ""}`);

  function resetBrowser() {
    requestGuard.invalidate("browse");
    setBrowser(emptyBrowserState);
  }

  function openBrowser(purpose) {
    const nextPath =
      purpose === "download"
        ? rememberedDownloadPath(runtimeTarget, defaultRemoteDir, normalizeRemoteDirectoryInput)
        : normalizeRemoteDirectoryInput(remoteDir || defaultRemoteDir);
    setBrowser({ open: true, purpose, path: nextPath, state: "loading", data: null, error: null });
    void loadBrowser(nextPath, purpose, { fallbackToDefault: purpose === "download" });
  }

  async function loadBrowser(pathValue = browser.path, purpose = browser.purpose, options = {}) {
    if (!runtimeTarget) return;
    const request = requestGuard.begin("browse");
    const nextPath = normalizeRemoteDirectoryInput(pathValue || "/");
    setBrowser((current) => ({ ...current, purpose, path: nextPath, state: options.append ? "loading-more" : "loading", error: null }));
    try {
      const data = await apiPost(
        "/api/file-transfers/browse",
        {
          runtime_id: Number(runtimeTarget.id),
          path: nextPath,
          ...(options.cursor ? { cursor: options.cursor } : {}),
        },
        { signal: request.signal },
      );
      if (!request.isCurrent()) return;
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
      setBrowser((current) => ({ ...current, purpose, path: nextPath, state: "error", error: error.message }));
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
    setBrowserPath(path) {
      setBrowser((current) => ({ ...current, path }));
    },
    useBrowserDirectory,
    resetBrowser,
  };
}
