import { useCallback, useState } from "react";
import { apiGet, apiPost } from "../lib/api";
import { failedResource, pollReadOptions } from "../lib/async-resource";
import { useRequestGuard } from "../lib/request-guard";
import { connectorApproval, connectorApprovals } from "../lib/gateway-contracts/security-contracts";
import type { ConnectorApproval } from "../lib/gateway-contracts/security-contracts";
import { backupFreshnessResponse, runtimeMessagesResponse } from "../lib/gateway-contracts/activity-resource-contracts.ts";
import type { BackupCheckError, BackupFreshnessItem, RuntimeMessage } from "../lib/gateway-contracts/activity-resource-contracts.ts";

const runApprovalStatuses = ["completed", "failed", "canceled", "running", "blocked", "stale", "error", "outcome_unknown"] as const;
const declineApprovalStatuses = ["declined"] as const;

type ListResource<Item> = { state: "loading" | "ready" | "error"; data: Item[]; error: string | null };
type Props = { pollIsCurrent: (_generation?: number) => boolean };

export function useGatewayActivityResources({ pollIsCurrent }: Props) {
  const [connectorActionApprovals, setConnectorActionApprovals] = useState<ListResource<ConnectorApproval>>({
    state: "loading",
    data: [],
    error: null,
  });
  const [messages, setMessages] = useState<ListResource<RuntimeMessage>>({ state: "loading", data: [], error: null });
  const [backupFreshness, setBackupFreshness] = useState<ListResource<BackupFreshnessItem> & { checkErrors: BackupCheckError[] }>({
    state: "loading",
    data: [],
    checkErrors: [],
    error: null,
  });
  const requests = useRequestGuard("gateway-activity-resources");

  const loadConnectorActionApprovals = useCallback(
    async (generation?: number) => {
      const request = requests.begin("connector-approvals");
      try {
        const data = await apiGet("/api/connector-action-approvals", pollReadOptions(request.signal, generation));
        if (!request.isCurrent() || !pollIsCurrent(generation)) return;
        setConnectorActionApprovals({ state: "ready", data: connectorApprovals(data), error: null });
      } catch (error) {
        if (!request.isCurrent() || !pollIsCurrent(generation)) return;
        setConnectorActionApprovals((current) => failedResource(current, error));
      } finally {
        request.complete();
      }
    },
    [pollIsCurrent, requests],
  );

  const loadMessages = useCallback(
    async (generation?: number) => {
      const request = requests.begin("messages");
      try {
        const data = await apiGet("/api/messages", pollReadOptions(request.signal, generation));
        if (!request.isCurrent() || !pollIsCurrent(generation)) return;
        setMessages({ state: "ready", data: runtimeMessagesResponse(data), error: null });
      } catch (error) {
        if (!request.isCurrent() || !pollIsCurrent(generation)) return;
        setMessages((current) => failedResource(current, error));
      } finally {
        request.complete();
      }
    },
    [pollIsCurrent, requests],
  );

  const loadBackupFreshness = useCallback(async () => {
    const request = requests.begin("backup-freshness");
    try {
      const data = await apiGet("/api/backup/freshness", { signal: request.signal });
      if (!request.isCurrent()) return;
      const freshness = backupFreshnessResponse(data);
      setBackupFreshness({ state: "ready", data: freshness.items, checkErrors: freshness.checkErrors, error: null });
    } catch (error) {
      if (!request.isCurrent()) return;
      setBackupFreshness((current) => failedResource(current, error));
    } finally {
      request.complete();
    }
  }, [requests]);

  const runConnectorActionApproval = useCallback(
    async (approval: ConnectorApproval, userNote = "") => {
      try {
        const item = connectorApproval(
          await apiPost(`/api/connector-action-approvals/${approval.id}/run`, {
            user_note: userNote,
            approval_context_hash: approval.approval_context_hash,
          }),
          {
            id: approval.id,
            targetRef: approval.target_ref,
            actionName: approval.action_name,
            statuses: runApprovalStatuses,
          },
        );
        await loadConnectorActionApprovals();
        return item;
      } catch (error) {
        await loadConnectorActionApprovals();
        throw error;
      }
    },
    [loadConnectorActionApprovals],
  );

  const declineConnectorActionApproval = useCallback(
    async (approval: ConnectorApproval, userNote = "") => {
      const item = connectorApproval(
        await apiPost(`/api/connector-action-approvals/${approval.id}/decline`, {
          user_note: userNote,
          approval_context_hash: approval.approval_context_hash,
        }),
        {
          id: approval.id,
          targetRef: approval.target_ref,
          actionName: approval.action_name,
          statuses: declineApprovalStatuses,
        },
      );
      await loadConnectorActionApprovals();
      return item;
    },
    [loadConnectorActionApprovals],
  );

  const markRuntimeMessagesRead = useCallback(
    async (runtimeID: string | number, messageIDs: readonly number[]) => {
      const result = await apiPost("/api/messages/read", { runtime_id: Number(runtimeID), message_ids: messageIDs });
      await loadMessages();
      return result;
    },
    [loadMessages],
  );

  return {
    backupFreshness,
    connectorActionApprovals,
    declineConnectorActionApproval,
    loadBackupFreshness,
    loadConnectorActionApprovals,
    loadMessages,
    markRuntimeMessagesRead,
    messages,
    runConnectorActionApproval,
    setBackupFreshness,
  };
}
