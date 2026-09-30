import { useEffect, useState } from "react";
import { apiGet, currentWorkspaceBinding } from "../../../lib/api";
import {
  listLocalActionRetryEntries,
  localActionRetryLedgerChangedEvent,
  localActionRetryObservationFailedEvent,
} from "../../../lib/local-action-retry";
import { retryEntryBlocksConsoleBatch } from "../../../lib/local-action-retry/records";
import { isDefinitiveConsoleCommandStatus, isUnknownConsoleCommandStatus } from "../../../lib/gateway-contracts/console-command-contract";
import { scheduleObservation } from "./observation-scheduler";
import type { RetryListEntry } from "../../../lib/local-action-retry";
import type { ObservationScheduler } from "./observation-scheduler";

function protectedBatch(entry: RetryListEntry) {
  return "invalid" in entry || retryEntryBlocksConsoleBatch(entry);
}

export function useConsoleBatchOwnership(active: boolean, observeCommands = true, schedule: ObservationScheduler = scheduleObservation) {
  const workspaceID = currentWorkspaceBinding();
  const [state, setState] = useState({ workspaceID: "", blocked: true, notice: "" });
  useEffect(() => {
    if (!active || !workspaceID) return;
    setState({ workspaceID, blocked: true, notice: "" });
    let alive = true;
    let inspecting = false;
    let cancelTimer: (() => void) | undefined;
    let controller: AbortController | undefined;
    const inspect = async () => {
      if (inspecting || !alive) return;
      inspecting = true;
      cancelTimer?.();
      controller = new AbortController();
      try {
        const entries = await listLocalActionRetryEntries(workspaceID);
        if (!alive || controller.signal.aborted) return;
        const commands = (
          observeCommands
            ? entries.flatMap((entry) =>
                "batch_requests" in entry && entry.state === "pending" && Array.isArray(entry.batch_requests)
                  ? entry.batch_requests.filter(
                      (item) =>
                        !isUnknownConsoleCommandStatus(item.status) && (!item.observed || !isDefinitiveConsoleCommandStatus(item.status)),
                    )
                  : [],
              )
            : []
        ).slice(0, 25);
        const observations = await Promise.allSettled(
          commands.map((command) =>
            apiGet(`/api/console/command-requests/${command.request_id}`, {
              workspaceBinding: workspaceID,
              signal: controller?.signal,
              timeoutMs: 10000,
            }),
          ),
        );
        if (observations.some((result) => result.status === "rejected")) throw new Error("Bulk observation failed");
        const blocked = (await listLocalActionRetryEntries(workspaceID)).some(protectedBatch);
        if (alive)
          setState({
            workspaceID,
            blocked,
            notice: blocked ? "A previous bulk operation remains protected. Review its results or reconcile it in Settings." : "",
          });
      } catch {
        if (alive)
          setState({
            workspaceID,
            blocked: true,
            notice: "Bulk operation status could not be verified. The previous operation remains protected.",
          });
      } finally {
        inspecting = false;
        if (alive) cancelTimer = schedule(inspect, 3000);
      }
    };
    void inspect();
    const changed = () => {
      void inspect();
    };
    window.addEventListener(localActionRetryLedgerChangedEvent, changed);
    window.addEventListener(localActionRetryObservationFailedEvent, changed);
    return () => {
      alive = false;
      cancelTimer?.();
      controller?.abort();
      window.removeEventListener(localActionRetryLedgerChangedEvent, changed);
      window.removeEventListener(localActionRetryObservationFailedEvent, changed);
    };
  }, [active, observeCommands, schedule, workspaceID]);
  return {
    workspaceID,
    locked: !active || !workspaceID || state.workspaceID !== workspaceID || state.blocked,
    notice: state.workspaceID === workspaceID ? state.notice : "",
  };
}
