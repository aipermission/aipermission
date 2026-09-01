import { Terminal } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { apiPost, apiUrl } from "../../lib/api";
import { limitTranscript } from "../app-shell-runtime";
import { PtyConsole } from "../console/pty-console";
import { Button } from "../ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../ui/card";
import { Dialog } from "../ui/dialog";
import { Notice } from "../ui/notice";

const closedSession = { transcript: "", status: "closed", error: null, shell: "" };
const maxPendingInputBytes = 64 * 1024;

export function MaintenanceConsolePanel() {
  const [open, setOpen] = useState(false);
  const [openError, setOpenError] = useState("");
  const socketRef = useRef(null);
  const socketReadyRef = useRef(false);
  const pendingInputRef = useRef("");
  const intentionallyClosedSocketsRef = useRef(new WeakSet());
  const lifecycleGenerationRef = useRef(0);
  const connectTimerRef = useRef(0);
  const openRequestedRef = useRef(false);
  const operationPendingRef = useRef(false);
  const [session, setSession] = useState(closedSession);

  useEffect(() => {
    const intentionallyClosedSockets = intentionallyClosedSocketsRef.current;
    const lifecycleGeneration = lifecycleGenerationRef;
    const connectTimer = connectTimerRef;
    const openRequested = openRequestedRef;
    const operationPending = operationPendingRef;
    return () => {
      lifecycleGeneration.current += 1;
      window.clearTimeout(connectTimer.current);
      if (socketRef.current) intentionallyClosedSockets.add(socketRef.current);
      socketRef.current?.close();
      socketRef.current = null;
      socketReadyRef.current = false;
      pendingInputRef.current = "";
      operationPending.current = false;
      if (openRequested.current) {
        openRequested.current = false;
        void apiPost("/api/settings/maintenance-console/close", {}).catch(() => undefined);
      }
    };
  }, []);

  async function openConsole() {
    if (operationPendingRef.current) return;
    operationPendingRef.current = true;
    openRequestedRef.current = true;
    const generation = ++lifecycleGenerationRef.current;
    setOpenError("");
    try {
      await apiPost("/api/settings/maintenance-console/open", {});
      if (generation !== lifecycleGenerationRef.current) {
        if (!openRequestedRef.current) await apiPost("/api/settings/maintenance-console/close", {}).catch(() => undefined);
        return;
      }
      setSession((current) => ({ ...current, status: "connecting", error: null }));
      setOpen(true);
      connectTimerRef.current = window.setTimeout(() => {
        if (generation === lifecycleGenerationRef.current) connect({ force: true });
      }, 0);
    } catch (error) {
      if (generation === lifecycleGenerationRef.current) {
        openRequestedRef.current = false;
        setOpenError(error.message);
      }
    } finally {
      if (generation === lifecycleGenerationRef.current) operationPendingRef.current = false;
    }
  }

  async function closeConsole() {
    lifecycleGenerationRef.current += 1;
    operationPendingRef.current = true;
    window.clearTimeout(connectTimerRef.current);
    setOpen(false);
    if (socketRef.current) intentionallyClosedSocketsRef.current.add(socketRef.current);
    socketRef.current?.close();
    socketRef.current = null;
    socketReadyRef.current = false;
    pendingInputRef.current = "";
    setSession(closedSession);
    try {
      await apiPost("/api/settings/maintenance-console/close", {});
      openRequestedRef.current = false;
    } catch (error) {
      setOpenError(`${error.message || "Maintenance console close failed."} Reopen the console and retry closing it.`);
    } finally {
      operationPendingRef.current = false;
    }
  }

  async function reconnect() {
    if (operationPendingRef.current) return;
    operationPendingRef.current = true;
    openRequestedRef.current = true;
    const generation = ++lifecycleGenerationRef.current;
    setOpenError("");
    try {
      await apiPost("/api/settings/maintenance-console/open", {});
      if (generation !== lifecycleGenerationRef.current) {
        if (!openRequestedRef.current) await apiPost("/api/settings/maintenance-console/close", {}).catch(() => undefined);
        return;
      }
      connect({ force: true });
    } catch (error) {
      if (generation === lifecycleGenerationRef.current) setOpenError(error.message);
    } finally {
      if (generation === lifecycleGenerationRef.current) operationPendingRef.current = false;
    }
  }

  function connect(options = {}) {
    const existing = socketRef.current;
    if (existing && (existing.readyState === WebSocket.OPEN || existing.readyState === WebSocket.CONNECTING)) {
      if (!options.force) return;
      intentionallyClosedSocketsRef.current.add(existing);
      existing.close();
    }
    const socket = new WebSocket(maintenanceConsoleAttachUrl());
    socketRef.current = socket;
    socketReadyRef.current = false;
    setSession((current) => ({ ...current, status: "connecting", error: null }));
    socket.onmessage = (event) => {
      if (socketRef.current !== socket) return;
      let message;
      try {
        message = JSON.parse(event.data);
      } catch {
        setSession((current) => ({ ...current, status: "error", error: "Maintenance console returned an invalid message." }));
        return;
      }
      if (message.type === "snapshot") {
        setSession({ transcript: message.data || "", status: message.status || "connected", error: null, shell: message.shell || "" });
      }
      if (message.type === "ready") {
        socketReadyRef.current = true;
        if (pendingInputRef.current) {
          socket.send(JSON.stringify({ type: "input", data: pendingInputRef.current }));
          pendingInputRef.current = "";
        }
        setSession((current) => ({
          ...current,
          status: message.status || "connected",
          shell: message.shell || current.shell,
          error: null,
        }));
      }
      if (message.type === "output") {
        setSession((current) => ({
          ...current,
          transcript: limitTranscript(`${current.transcript || ""}${message.data || ""}`),
          status: message.status || "connected",
          shell: message.shell || current.shell,
          error: null,
        }));
      }
      if (message.type === "error") {
        setSession((current) => ({
          ...current,
          transcript: limitTranscript(`${current.transcript || ""}\r\n${message.data || "Maintenance console error"}\r\n`),
          status: "error",
          error: message.data || "Maintenance console error",
        }));
      }
      if (message.type === "exit") {
        setSession((current) => ({ ...current, status: message.status || "closed", error: message.data || "" }));
      }
    };
    socket.onerror = () => {
      if (socketRef.current !== socket) return;
      setSession((current) => ({ ...current, status: "error", error: "Maintenance console connection failed." }));
    };
    socket.onclose = () => {
      const intentional = intentionallyClosedSocketsRef.current.has(socket);
      intentionallyClosedSocketsRef.current.delete(socket);
      if (socketRef.current !== socket) return;
      socketRef.current = null;
      socketReadyRef.current = false;
      if (!intentional) {
        setSession((current) => ({
          ...current,
          status: "disconnected",
          error: current.error || "Maintenance console connection closed. Reconnecting will preserve queued input.",
        }));
      }
    };
  }

  function sendInput(data) {
    const socket = socketRef.current;
    if (socket?.readyState === WebSocket.OPEN && socketReadyRef.current) {
      socket.send(JSON.stringify({ type: "input", data }));
      return;
    }
    if (new Blob([pendingInputRef.current, data]).size > maxPendingInputBytes) {
      setSession((current) => ({ ...current, status: "error", error: "Maintenance console input queue is full." }));
      return;
    }
    pendingInputRef.current += data;
    setSession((current) => ({ ...current, status: "connecting", error: null }));
    connect();
  }

  function resize(cols, rows) {
    const socket = socketRef.current;
    if (socket?.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ type: "resize", cols, rows }));
  }

  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle>Maintenance console</CardTitle>
          <CardDescription>Open a realtime local terminal inside the AIPermission gateway runtime.</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4">
          <Notice tone="warn">
            Local UI-only diagnostics for the gateway runtime. It is not exposed to MCP, output is bounded in memory, and open/close
            lifecycle events are audited.
          </Notice>
          <Button type="button" onClick={openConsole}>
            <Terminal className="h-4 w-4" />
            Open maintenance console
          </Button>
          {openError ? <Notice tone="bad">{openError}</Notice> : null}
        </CardContent>
      </Card>
      <Dialog
        open={open}
        title="Maintenance console"
        description="Interactive local terminal inside the gateway container."
        onClose={closeConsole}
        size="wide"
        className="h-[calc(100vh-100px)] !w-[85vw] !max-w-[1600px] grid-rows-[auto_minmax(0,1fr)]"
        bodyClassName="min-h-0 p-0"
        closeOnOverlay={false}
      >
        <div className="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)_auto]">
          <div className="border-b border-stone-200 p-4">
            <Notice tone="warn" className="py-2 text-xs">
              Local UI-only diagnostics for the gateway runtime. It is not exposed to MCP. Avoid printing secrets in this terminal.
            </Notice>
          </div>
          <div className="min-h-0">
            <PtyConsole session={session} onInput={sendInput} onResize={resize} theme="dark" />
          </div>
          <div className="flex flex-wrap items-center justify-between gap-3 border-t border-stone-200 px-4 py-3 text-xs text-stone-500">
            <div className="flex min-w-0 flex-wrap items-center gap-2">
              <span className="inline-flex items-center gap-1 rounded-full border border-stone-200 px-2 py-1 font-semibold text-stone-700">
                <Terminal className="h-3.5 w-3.5" />
                {session.status || "closed"}
              </span>
              {session.shell ? <span className="truncate font-mono">{session.shell}</span> : null}
              {session.error || openError ? <span className="truncate text-red-600">{session.error || openError}</span> : null}
            </div>
            <Button type="button" variant="outline" className="h-8 px-3 text-xs" onClick={reconnect}>
              Reconnect
            </Button>
          </div>
        </div>
      </Dialog>
    </>
  );
}

function maintenanceConsoleAttachUrl() {
  const url = new URL(apiUrl, window.location.origin);
  url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
  url.pathname = "/api/settings/maintenance-console/attach";
  return url.toString();
}
