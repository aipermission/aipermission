import { useCallback, useEffect, useMemo, useState } from "react";
import { connectorTargetKey, profilesForConnectorTarget } from "../../lib/connector-permissions";
import {
  consoleTargetRows,
  defaultConsoleTargetRef,
  groupConsoleTargetsByProject,
  targetDisplayName,
  targetSubtitle,
  targetUsesLiveConsole,
} from "./console-target-sidebar";
import { isUnreadMessage } from "./helpers";

type Target = {
  ref: string;
  connector_kind: string;
  target_id: number;
  profile_id: number;
  profile_label: string;
  runtime_id?: number | string;
  project_name?: string;
  project_slug?: string;
};
type Message = { direction: string; consumed_at?: string | null };
type Props = {
  messages: { data: Message[] };
  pendingApprovals: readonly unknown[];
  selectedTargetRef: string;
  setSearchParams: (_params: { target: string }, _options?: { replace: boolean }) => void;
  targets: { data: Target[] } | null;
};

export function useConsoleTargetSelection({ messages, pendingApprovals, selectedTargetRef, setSearchParams, targets }: Props) {
  const [profileByTarget, setProfileByTarget] = useState<Record<string, number>>({});
  const [search, setSearch] = useState("");
  const [collapsedProjects, setCollapsedProjects] = useState<Record<number, boolean>>({});
  const targetItems = useMemo<Target[]>(() => targets?.data || [], [targets?.data]);
  const unreadMessages = useMemo(() => messages.data.filter(isUnreadMessage), [messages.data]);
  const defaultTargetRef = useMemo(
    () => defaultConsoleTargetRef(targetItems, unreadMessages, pendingApprovals),
    [pendingApprovals, targetItems, unreadMessages],
  );
  const selectedTarget = useMemo(() => {
    if (targetItems.length === 0) return null;
    const exact = selectedTargetRef ? targetItems.find((target) => target.ref === selectedTargetRef) : null;
    return exact || targetItems.find((target) => target.ref === defaultTargetRef) || targetItems[0];
  }, [defaultTargetRef, selectedTargetRef, targetItems]);
  const selectedProfiles = useMemo<Target[]>(() => profilesForConnectorTarget(targetItems, selectedTarget), [selectedTarget, targetItems]);
  const targetRows = useMemo<Target[]>(
    () => consoleTargetRows(targetItems, selectedTarget, profileByTarget),
    [profileByTarget, selectedTarget, targetItems],
  );
  const filteredTargets = useMemo(() => {
    const query = search.trim().toLowerCase();
    return targetRows.filter((target) => {
      if (!query) return true;
      const profiles: Target[] = profilesForConnectorTarget(targetItems, target);
      return [
        target.project_name,
        target.project_slug,
        targetDisplayName(target),
        targetSubtitle(target),
        target.connector_kind,
        target.ref,
        ...profiles.map((profile) => profile.profile_label),
      ]
        .filter(Boolean)
        .some((value) => String(value).toLowerCase().includes(query));
    });
  }, [search, targetItems, targetRows]);
  const groups = useMemo(() => groupConsoleTargetsByProject(filteredTargets), [filteredTargets]);

  useEffect(() => {
    if (targetItems.length === 0 || !defaultTargetRef) return;
    if (!selectedTargetRef || !targetItems.some((target) => target.ref === selectedTargetRef)) {
      setSearchParams({ target: selectedTarget?.ref || defaultTargetRef }, { replace: true });
    }
  }, [defaultTargetRef, selectedTarget, selectedTargetRef, setSearchParams, targetItems]);

  useEffect(() => {
    if (!selectedTarget?.profile_id) return;
    const key = connectorTargetKey(selectedTarget);
    setProfileByTarget((current) =>
      String(current[key] || "") === String(selectedTarget.profile_id) ? current : { ...current, [key]: Number(selectedTarget.profile_id) },
    );
  }, [selectedTarget]);

  const selectTarget = useCallback(
    (target: Target | null) => {
      if (!target) return;
      const profiles: Target[] = profilesForConnectorTarget(targetItems, target);
      const selectedProfileID = profileByTarget[connectorTargetKey(target)] || target.profile_id;
      const profileTarget = profiles.find((profile) => Number(profile.profile_id) === Number(selectedProfileID)) || profiles[0] || target;
      setSearchParams({ target: profileTarget.ref });
    },
    [profileByTarget, setSearchParams, targetItems],
  );

  const selectProfile = useCallback(
    (profileID: number | string) => {
      if (!selectedTarget) return;
      const nextID = Number(profileID);
      if (!Number.isFinite(nextID) || nextID <= 0) return;
      const profileTarget = selectedProfiles.find((profile) => Number(profile.profile_id) === nextID);
      if (!profileTarget) return;
      setProfileByTarget((current) => ({ ...current, [connectorTargetKey(selectedTarget)]: nextID }));
      setSearchParams({ target: profileTarget.ref });
    },
    [selectedProfiles, selectedTarget, setSearchParams],
  );

  const toggleProject = useCallback((projectID: number) => {
    setCollapsedProjects((current) => ({ ...current, [projectID]: !current[projectID] }));
  }, []);

  return {
    collapsedProjects,
    filteredTargets,
    groups,
    search,
    selectProfile,
    selectTarget,
    selectedProfiles,
    selectedRuntimeID: selectedTarget && targetUsesLiveConsole(selectedTarget) ? String(selectedTarget.runtime_id || "") : "",
    selectedTarget,
    setSearch,
    targetItems,
    targetRows,
    toggleProject,
    unreadMessages,
  };
}
