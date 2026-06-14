import { Files, ListChecks, MessageSquare, RefreshCcw, Square, XCircle } from "lucide-react";
import { useState } from "react";
import { CountBadge } from "../../../components/ui/badge";
import { Button } from "../../../components/ui/button";
import { BulkCommandDialog } from "./bulk-command-dialog";
import { FileTransferDialog } from "./file-transfer-dialog";

export function SSHConnectorConsoleTemplate({ children }) {
  return children;
}

export function SSHConnectorToolbarActionsTemplate({
  theme,
  selectedRuntimeTarget,
  selectedSession,
  selectedSessionLive,
  selectedUnreadMessages = [],
  onOpenMessages,
  onRefreshSessions,
  onNewSession,
  onEndSession,
  onInterrupt,
  liveConsoleTargets = [],
}) {
  const [bulkOpen, setBulkOpen] = useState(false);
  const [filesOpen, setFilesOpen] = useState(false);
  const buttonClass = `h-9 border px-3 ${theme === "light" ? "border-stone-300 text-stone-800 hover:bg-stone-100" : "border-stone-600 text-stone-100 hover:bg-stone-700"}`;

  return (
    <>
      <Button
        type="button"
        variant="ghost"
        className={`relative h-9 border px-3 ${
          selectedUnreadMessages.length > 0
            ? "border-red-500/70 bg-red-950/30 text-red-100 hover:bg-red-900/40"
            : theme === "light"
              ? "border-stone-300 text-stone-800 hover:bg-stone-100"
              : "border-stone-600 text-stone-100 hover:bg-stone-700"
        }`}
        onClick={() => onOpenMessages?.()}
        disabled={!selectedRuntimeTarget}
      >
        <MessageSquare className="h-3.5 w-3.5" />
        Messages
        {selectedUnreadMessages.length > 0 ? <CountBadge className="absolute -right-1 -top-1">{selectedUnreadMessages.length}</CountBadge> : null}
      </Button>
      <Button
        type="button"
        variant="ghost"
        className={buttonClass}
        onClick={() => setBulkOpen(true)}
        disabled={liveConsoleTargets.length === 0}
        title="Run one SSH command across selected SSH connectors"
      >
        <ListChecks className="h-3.5 w-3.5" />
        Bulk
      </Button>
      <Button
        type="button"
        variant="ghost"
        className={buttonClass}
        onClick={() => setFilesOpen(true)}
        disabled={!selectedRuntimeTarget}
        title="Upload or download files over SSH"
      >
        <Files className="h-3.5 w-3.5" />
        Files
      </Button>
      <Button type="button" variant="ghost" className={buttonClass} onClick={onNewSession} disabled={!selectedRuntimeTarget}>
        <RefreshCcw className="h-3.5 w-3.5" />
        New Session
      </Button>
      <Button
        type="button"
        variant="ghost"
        className={buttonClass}
        onClick={onEndSession}
        disabled={!selectedSessionLive}
        title="Close the SSH shell session"
      >
        <XCircle className="h-3.5 w-3.5" />
        End Session
      </Button>
      <Button
        type="button"
        variant="ghost"
        className={buttonClass}
        onClick={onInterrupt}
        disabled={!selectedSessionLive || selectedSession?.status !== "connected"}
        title="Send Ctrl+C to the running SSH command"
      >
        <Square className="h-3.5 w-3.5" />
        Interrupt
      </Button>
      <FileTransferDialog open={filesOpen} server={selectedRuntimeTarget} onClose={() => setFilesOpen(false)} />
      <BulkCommandDialog
        open={bulkOpen}
        targets={liveConsoleTargets}
        selectedTarget={selectedRuntimeTarget}
        onClose={() => setBulkOpen(false)}
        onRefresh={onRefreshSessions}
      />
    </>
  );
}
