import { useCallback, useState } from "react";
import { apiGet, apiPost } from "../lib/api";
import { failedResource, pollReadOptions } from "../lib/async-resource";
import { useRequestGuard } from "../lib/request-guard";
import { connectorApproval, connectorApprovals } from "../lib/gateway-contracts/security-contracts";

const runApprovalStatuses = ["completed", "failed", "canceled", "running", "blocked", "stale", "error", "outcome_unknown"];
const declineApprovalStatuses = ["declined"];

const loadingList = { state: "loading", data: [], error: null };

export function useGatewayActivityResources({ pollIsCurrent }) {
  const [connectorActionApprovals, setConnectorActionApprovals] = useState(loadingList);
  const [messages, setMessages] = useState(loadingList);
  const [backupFreshness, setBackupFreshness] = useState({ state: "loading", data: [], checkErrors: [], error: null });
  const requests = useRequestGuard("gateway-activity-resources");

  const loadConnectorActionApprovals = useCallback(
    async (generation) => {
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
    async (generation) => {
      const request = requests.begin("messages");
      try {
        const data = await apiGet("/api/messages", pollReadOptions(request.signal, generation));
        if (!request.isCurrent() || !pollIsCurrent(generation)) return;
        setMessages({ state: "ready", data, error: null });
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
      setBackupFreshness({ state: "ready", data: data?.items || [], checkErrors: data?.check_errors || [], error: null });
    } catch (error) {
      if (!request.isCurrent()) return;
      setBackupFreshness((current) => failedResource(current, error));
    } finally {
      request.complete();
    }
  }, [requests]);

  const runConnectorActionApproval = useCallback(
    async (approval, userNote = "") => {
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
    async (approval, userNote = "") => {
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
    async (runtimeID) => {
      const result = await apiPost("/api/messages/read", { runtime_id: Number(runtimeID) });
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
