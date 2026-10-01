import { useCallback, useEffect, useRef, useState } from "react";
import { apiPost, currentWorkspaceBinding } from "../../../../lib/api";
import { useRequestGuard } from "../../../../lib/request-guard";
import { roleHistoryPage } from "./role-history-contract";
import { canSubmitRoleDecision, confirmedRoleDecision } from "./role-reconciliation-contract";
import type { RoleDecisionMode } from "./role-reconciliation-contract";
import type { RoleHistoryEntry, RoleHistoryPage } from "./role-history-types";

const maximumSavedCursors = 64;
const workspaceNotice = "Workspace changed. Close this dialog and reopen role history.";
type HistoryState = {
  targetID: number;
  state: "loading" | "ready" | "error";
  page: RoleHistoryPage | null;
  error: string;
  cursors: string[];
  pageOffset: number;
};

function emptyHistory(targetID: number, cursors = ["0"], pageOffset = 0): HistoryState {
  return { targetID, state: "loading", page: null, error: "", cursors, pageOffset };
}

export function useRoleHistory(targetID: number, workspaceBinding?: string) {
  const [workspace] = useState(() => workspaceBinding ?? currentWorkspaceBinding());
  const observedWorkspace = currentWorkspaceBinding();
  const drifted = useRef(false);
  const navigationOwner = useRef({});
  const scopeOwner = useRef({});
  const pending = useRef<{ scope: object } | null>(null);
  const selectedTarget = useRef(targetID);
  if (selectedTarget.current !== targetID) {
    selectedTarget.current = targetID;
    navigationOwner.current = {};
    scopeOwner.current = {};
  }
  const guard = useRequestGuard(JSON.stringify([workspace, targetID]));
  const [snapshot, setSnapshot] = useState(() => emptyHistory(targetID));
  const [decision, setDecision] = useState<{ scope: object; busy: boolean; notice: string } | null>(null);
  const checkWorkspace = useCallback(() => {
    if (!drifted.current && currentWorkspaceBinding() === workspace) return true;
    if (!drifted.current) {
      drifted.current = true;
      navigationOwner.current = {};
      guard.invalidate("history");
      guard.invalidate("decision");
      setSnapshot({ ...emptyHistory(selectedTarget.current), state: "error", error: workspaceNotice });
    }
    return false;
  }, [guard, workspace]);
  const load = useCallback(
    async (cursors: string[], pageOffset = 0, mutation?: { scope: object }) => {
      if (selectedTarget.current !== targetID || (pending.current?.scope === scopeOwner.current && pending.current !== mutation)) return;
      navigationOwner.current = {};
      const request = guard.begin("history");
      if (!request.isCurrent() || !checkWorkspace()) {
        request.complete();
        return;
      }
      setSnapshot(emptyHistory(targetID, cursors, pageOffset));
      const isCurrent = () => request.isCurrent() && selectedTarget.current === targetID && checkWorkspace();
      try {
        if (!Number.isSafeInteger(targetID) || targetID <= 0) throw new Error("Invalid Postgres role history target.");
        const after = cursors.at(-1) || "0";
        const raw = await apiPost(
          `/api/connector-targets/${targetID}/operations/role-lifecycle-status`,
          after === "0" ? {} : { after_resource_id: after },
          { signal: request.signal, workspaceBinding: workspace },
        );
        if (!isCurrent()) return;
        const page = roleHistoryPage(raw, targetID, after);
        setSnapshot({ targetID, state: "ready", page, error: "", cursors, pageOffset });
      } catch {
        if (isCurrent())
          setSnapshot({ ...emptyHistory(targetID, cursors, pageOffset), state: "error", error: "Unable to load Postgres role history." });
      } finally {
        request.complete();
      }
    },
    [checkWorkspace, guard, targetID, workspace],
  );

  useEffect(() => {
    void load(["0"]);
    return () => guard.invalidate("history");
  }, [guard, load]);
  useEffect(() => {
    checkWorkspace();
    const timer = window.setInterval(checkWorkspace, 500);
    return () => window.clearInterval(timer);
  }, [checkWorkspace, observedWorkspace]);

  const workspaceChanged = drifted.current || observedWorkspace !== workspace;
  const scope = scopeOwner.current;
  const busy = !workspaceChanged && decision?.scope === scope && decision.busy;
  const state = workspaceChanged
    ? { ...emptyHistory(targetID), state: "error" as const, error: workspaceNotice }
    : snapshot.targetID === targetID
      ? snapshot
      : emptyHistory(targetID);
  const canPrevious = !busy && state.state === "ready" && state.cursors.length > 1;
  const pageNumber = state.pageOffset + state.cursors.length;
  const canFirst = !busy && !workspaceChanged && state.state !== "loading" && pageNumber > 1;
  const canNext = !busy && state.state === "ready" && Boolean(state.page?.has_more);
  const owner = navigationOwner.current;
  // Reject a retained snapshot's actions before granting new request ownership.
  const navigate = (cursors: string[], offset = 0) => (navigationOwner.current === owner ? load(cursors, offset) : Promise.resolve());
  const next = () => {
    if (!canNext || !state.page) return Promise.resolve();
    const cursors = [...state.cursors, state.page.next_after_resource_id];
    const evicted = Math.max(0, cursors.length - maximumSavedCursors);
    return navigate(cursors.slice(evicted), state.pageOffset + evicted);
  };
  const decide = async (entry: RoleHistoryEntry, profileID: number, mode: RoleDecisionMode, confirmedRoleName?: string) => {
    const cleanup = mode === "cleanup";
    if (
      navigationOwner.current !== owner ||
      selectedTarget.current !== targetID ||
      scopeOwner.current !== scope ||
      pending.current?.scope === scope ||
      state.state !== "ready" ||
      !state.page?.entries.includes(entry) ||
      !canSubmitRoleDecision(entry, profileID, mode, confirmedRoleName) ||
      !checkWorkspace()
    )
      return;
    let expected: RoleHistoryEntry;
    try {
      expected = roleHistoryPage({ target_id: targetID, entries: [entry], has_more: false, next_after_resource_id: "" }, targetID)
        .entries[0]!;
    } catch {
      return;
    }
    const mutation = { scope };
    pending.current = mutation;
    const request = guard.begin("decision");
    if (!request.isCurrent() || !checkWorkspace()) {
      pending.current = null;
      request.complete();
      return;
    }
    const isCurrent = () => request.isCurrent() && scopeOwner.current === scope && checkWorkspace();
    setDecision({ scope, busy: true, notice: "" });
    let notice = "";
    try {
      try {
        const raw = await apiPost(
          `/api/connector-targets/${targetID}/operations/role-lifecycle-${cleanup ? "cleanup" : "reconcile"}`,
          { profile_id: String(profileID), input: { expected, ...(cleanup ? { confirmed_role_name: confirmedRoleName } : {}) } },
          { signal: request.signal, workspaceBinding: workspace },
        );
        if (!isCurrent()) return;
        confirmedRoleDecision(raw, expected, targetID, mode);
        notice = cleanup ? "Remote role cleanup acknowledged. Local credentials were not changed." : "Exact role presence confirmed.";
      } catch {
        if (!isCurrent()) return;
        notice =
          "Decision outcome was not confirmed. Inspect current role evidence and audit before another decision. No automatic retry was made.";
      }
      if (!isCurrent()) return;
      await load(state.cursors, state.pageOffset, mutation);
      if (isCurrent()) setDecision({ scope, busy: false, notice });
    } finally {
      if (pending.current === mutation) pending.current = null;
      if (isCurrent()) setDecision({ scope, busy: false, notice });
      request.complete();
    }
  };
  return {
    ...state,
    pageNumber,
    workspaceChanged,
    busy: Boolean(busy),
    notice: decision?.scope === scope && !workspaceChanged ? decision.notice : "",
    canFirst,
    canPrevious,
    canNext,
    refresh: () => navigate(state.cursors, state.pageOffset),
    first: () => (canFirst ? navigate(["0"]) : Promise.resolve()),
    previous: () => (canPrevious ? navigate(state.cursors.slice(0, -1), state.pageOffset) : Promise.resolve()),
    next,
    reconcile: (entry: RoleHistoryEntry, profileID: number) => decide(entry, profileID, "presence"),
    cleanup: (entry: RoleHistoryEntry, profileID: number, confirmedRoleName?: string) =>
      decide(entry, profileID, "cleanup", confirmedRoleName),
  };
}
