import { useCallback, useEffect, useRef, useState } from "react";
import { apiGet, apiPost, apiUrl, currentWorkspaceBinding } from "../../lib/api";
import { errorMessage } from "../../lib/errors";
import { useRequestGuard } from "../../lib/request-guard";
import { subscribeUISessionInvalidation } from "../../lib/ui-session-events";
import { capacityReport, closeResult, type Report, type Session } from "./model";

export function useConsoleCapacity() {
  const [data, setData] = useState<Report | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);
  const [open, setOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const [visible, setVisible] = useState(document.visibilityState !== "hidden");
  const [generation, setGeneration] = useState(0);
  const [authenticated, setAuthenticated] = useState(true);
  const guard = useRequestGuard("console-capacity");
  const busy = useRef(false);
  const refresh = useCallback(() => setGeneration((value) => value + 1), []);

  useEffect(() => {
    const visibility = () => setVisible(document.visibilityState !== "hidden");
    document.addEventListener("visibilitychange", visibility);
    const unsubscribe = subscribeUISessionInvalidation(() => {
      guard.invalidate("watch");
      guard.invalidate("close");
      setAuthenticated(false);
      setData(null);
      setOpen(false);
      setSaving(false);
    });
    return () => {
      document.removeEventListener("visibilitychange", visibility);
      unsubscribe();
    };
  }, [guard]);

  useEffect(() => {
    if (!visible || !authenticated) return;
    let disposed = false;
    let timer = 0;
    let reconnect = 0;
    let socket: WebSocket | null = null;
    let reading = false;
    let dirty = false;
    let revision = 0;
    const workspaceBinding = currentWorkspaceBinding();
    async function read() {
      if (disposed) return;
      if (reading) {
        dirty = true;
        return;
      }
      reading = true;
      dirty = false;
      const request = guard.begin("watch");
      try {
        const report = capacityReport(
          await apiGet("/api/console/capacity", {
            signal: request.signal,
            timeoutMs: 4000,
            workspaceBinding,
          }),
        );
        if (!request.isCurrent() || disposed || currentWorkspaceBinding() !== workspaceBinding) return;
        revision = report.revision;
        setData(report);
        setError(null);
      } catch (failure) {
        if (!request.isCurrent() || disposed) return;
        setError(errorMessage(failure, "Capacity unavailable."));
      } finally {
        reading = false;
        request.complete();
        if (dirty && !disposed) scheduleRead();
      }
    }
    function scheduleRead() {
      window.clearTimeout(timer);
      timer = window.setTimeout(() => void read(), 250);
    }
    function connect() {
      if (disposed) return;
      const url = new URL(apiUrl, window.location.origin);
      url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
      url.pathname = "/api/console/capacity/attach";
      url.searchParams.set("workspace", workspaceBinding);
      let current: WebSocket;
      try {
        current = new WebSocket(url.toString());
      } catch {
        reconnect = window.setTimeout(connect, 5000);
        return;
      }
      socket = current;
      current.onmessage = (event) => {
        try {
          const notice: unknown = JSON.parse(String(event.data));
          if (
            notice &&
            typeof notice === "object" &&
            "revision" in notice &&
            Number.isSafeInteger(notice.revision) &&
            notice.revision !== revision
          )
            scheduleRead();
        } catch {
          /* Invalid notifications never authorize or trigger cleanup. */
        }
      };
      current.onclose = () => {
        if (!disposed) reconnect = window.setTimeout(connect, 5000);
      };
      current.onerror = () => current.close();
    }
    void read();
    connect();
    const reconciliation = window.setInterval(() => void read(), 30_000);
    return () => {
      disposed = true;
      window.clearTimeout(timer);
      window.clearTimeout(reconnect);
      window.clearInterval(reconciliation);
      socket?.close();
      guard.invalidate("watch");
    };
  }, [authenticated, generation, guard, visible]);

  const close = useCallback(
    async (items: Session[], idleMinutes: number) => {
      if (busy.current || items.length === 0) return;
      busy.current = true;
      const request = guard.begin("close");
      const workspaceBinding = currentWorkspaceBinding();
      setSaving(true);
      setMessage(null);
      try {
        const result = closeResult(
          await apiPost(
            "/api/console/capacity/close",
            {
              idle_minutes: idleMinutes,
              sessions: items.map((item) => ({ id: item.id, generation: item.generation, last_activity: item.last_activity })),
            },
            { signal: request.signal, workspaceBinding },
          ),
        );
        if (!request.isCurrent() || currentWorkspaceBinding() !== workspaceBinding) return;
        setMessage(
          `${result.closed.length} closed, ${result.skipped.length} changed or unavailable, ${result.uncertain.length} require verification.`,
        );
      } catch (failure) {
        if (request.isCurrent())
          setMessage(
            `Cleanup could not be confirmed and may already have run. Verification is required before retrying. ${errorMessage(failure, "Refresh capacity.")}`,
          );
      } finally {
        guard.invalidate("watch");
        busy.current = false;
        if (request.isCurrent()) {
          setData(null);
          setSaving(false);
          refresh();
        }
        request.complete();
      }
    },
    [guard, refresh],
  );

  return { data, error, message, open, setOpen, saving, refresh, close };
}
