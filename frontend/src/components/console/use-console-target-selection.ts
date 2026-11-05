import { useCallback, useEffect, useMemo, useState } from "react";
import { connectorTargetKey, profilesForConnectorTarget } from "../../lib/connector-permissions";
import {
  consoleTargetRows,
  groupConsoleTargetsByProject,
  targetDisplayName,
  targetSubtitle,
  targetUsesLiveConsole,
} from "./console-target-sidebar";
import { isUnreadMessage } from "./helpers";
import type { ConsoleNavigationTarget } from "./console-target-sidebar";
import type { RuntimeMessage } from "../../lib/gateway-contracts/activity-resource-contracts";

type SelectionTarget = ConsoleNavigationTarget & {
  ref: string;
  connector_kind: string;
  target_id: number;
  profile_id: number;
  profile_label: string;
  runtime_id?: number | string;
  project_name?: string;
  project_slug?: string;
};
type Message = Pick<RuntimeMessage, "direction" | "consumed_at" | "runtime_id">;
type Props<Target, MessageItem extends Message> = {
  messages: { data: MessageItem[] };
  selectedTargetRef: string;
  setSearchParams: (_params: { target: string }, _options?: { replace: boolean }) => void;
  targets: { data: Target[] } | null;
};

export function useConsoleTargetSelection<Target extends SelectionTarget, MessageItem extends Message = Message>({
  messages,
  selectedTargetRef,
  setSearchParams,
  targets,
}: Props<Target, MessageItem>) {
  const [profileByTarget, setProfileByTarget] = useState<Record<string, number>>({});
  const [search, setSearch] = useState("");
  const [collapsedProjects, setCollapsedProjects] = useState<Record<string, boolean>>({});
  const targetItems = useMemo<Target[]>(() => targets?.data || [], [targets?.data]);
  const unreadMessages = useMemo(() => messages.data.filter(isUnreadMessage), [messages.data]);
  const selectedTarget = useMemo(
    () => (selectedTargetRef ? targetItems.find((target) => target.ref === selectedTargetRef) || null : null),
    [selectedTargetRef, targetItems],
  );
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

  const toggleProject = useCallback((projectID: string) => {
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
