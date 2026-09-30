import { useCallback, useEffect, useRef, useState } from "react";
import { apiPost, currentWorkspaceBinding } from "../../../lib/api";
import { errorMessage } from "../../../lib/errors";
import { useRequestGuard } from "../../../lib/request-guard";
import { assertCleanupAcknowledgement, cleanupSnapshot } from "./cleanup-contracts";
import type { CleanupSnapshot, CleanupSubmission } from "./cleanup-types";

export function useCleanupReconciliation(targetID: number) {
  const requests = useRequestGuard(`ssh-cleanup:${targetID}`);
  const pending = useRef<{ targetID: number } | null>(null);
  const source = useRef<{ targetID: number; workspace: string } | null>(null);
  const [snapshot, setSnapshot] = useState<CleanupSnapshot | null>(null);
  const [phase, setPhase] = useState("loading");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [revision, setRevision] = useState(0);
  const path = `/api/connector-targets/${targetID}/operations/`;
  const begin = useCallback(() => {
    const request = requests.begin("operation");
    const workspace = currentWorkspaceBinding();
    return {
      ...request,
      workspace,
      isCurrent: () => request.isCurrent() && currentWorkspaceBinding() === workspace,
      complete() {
        if (request.isCurrent() && currentWorkspaceBinding() !== workspace) {
          source.current = null;
          setSnapshot(null);
          setPhase("error");
          setError("The database changed; reopen the original database to inspect any pending decision, or reload current evidence.");
        }
        request.complete();
      },
    };
  }, [requests]);
  const read = useCallback(
    async (signal: AbortSignal) => cleanupSnapshot(await apiPost(`${path}key-cleanup-status`, {}, { signal }), targetID),
    [path, targetID],
  );

  const refresh = useCallback(async () => {
    if (pending.current?.targetID === targetID) return;
    const request = begin();
    source.current = null;
    setPhase("loading");
    setSnapshot(null);
    setError("");
    try {
      const value = await read(request.signal);
      if (!request.isCurrent()) return;
      source.current = { targetID, workspace: request.workspace };
      setSnapshot(value);
      setRevision((value) => value + 1);
      setPhase("ready");
    } catch (error) {
      if (!request.isCurrent()) return;
      setError(errorMessage(error));
      setPhase("error");
    } finally {
      request.complete();
    }
  }, [read, begin, targetID]);

  useEffect(() => {
    void refresh();
    return () => requests.invalidate("operation");
  }, [refresh, requests]);

  async function submit(input: CleanupSubmission) {
    if (pending.current?.targetID === targetID) return;
    if (source.current?.targetID !== targetID || source.current.workspace !== currentWorkspaceBinding()) {
      setSnapshot(null);
      setError("The database changed; reload cleanup evidence before recording a decision.");
      setPhase("error");
      return;
    }
    const mutation = { targetID };
    const record = snapshot?.records.find((value) => value.entry.resource_id === input.resource_id);
    const choice = record?.choices.find((value) => value.digest === input.identity_digest);
    if (
      !choice ||
      record?.entry.record.generation !== input.generation ||
      snapshot?.deletion_context_digest !== input.deletion_context_digest
    ) {
      setError("Reload SSH cleanup evidence before submitting.");
      return;
    }
    pending.current = mutation;
    const request = begin();
    setPhase("submitting");
    setError("");
    setNotice("");
    let acknowledgement = false;
    try {
      try {
        const value = await apiPost(`${path}key-cleanup-attest`, input, { signal: request.signal });
        assertCleanupAcknowledgement(value, input, choice);
        acknowledgement = true;
      } catch (error) {
        if (!request.isCurrent()) return;
        setNotice(
          `The decision outcome was not confirmed. ${errorMessage(error)} Inspect the recorded evidence and audit before another decision. No automatic retry was made.`,
        );
      }
      if (!request.isCurrent()) return;
      setSnapshot(null);
      source.current = null;
      const value = await read(request.signal);
      if (!request.isCurrent()) return;
      source.current = { targetID, workspace: request.workspace };
      setSnapshot(value);
      setRevision((value) => value + 1);
      setPhase("ready");
      if (acknowledgement) setNotice("Decision recorded. Current cleanup evidence reloaded.");
    } catch (error) {
      if (!request.isCurrent()) return;
      setError(`Current evidence could not be reloaded. ${errorMessage(error)}`);
      setPhase("error");
      if (acknowledgement) setNotice("The decision was acknowledged; reload current evidence before continuing.");
    } finally {
      if (pending.current === mutation) pending.current = null;
      request.complete();
    }
  }

  return { snapshot, phase, error, notice, revision, refresh, submit };
}
