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

export function useConsoleTargetSelection({ messages, pendingApprovals, selectedTargetRef, setSearchParams, targets }) {
  const [profileByTarget, setProfileByTarget] = useState({});
  const [search, setSearch] = useState("");
  const [collapsedProjects, setCollapsedProjects] = useState({});
  const targetItems = useMemo(() => targets?.data || [], [targets?.data]);
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
  const selectedProfiles = useMemo(() => profilesForConnectorTarget(targetItems, selectedTarget), [selectedTarget, targetItems]);
  const targetRows = useMemo(
    () => consoleTargetRows(targetItems, selectedTarget, profileByTarget),
    [profileByTarget, selectedTarget, targetItems],
  );
  const filteredTargets = useMemo(() => {
    const query = search.trim().toLowerCase();
    return targetRows.filter((target) => {
      if (!query) return true;
      const profiles = profilesForConnectorTarget(targetItems, target);
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
    (target) => {
      if (!target) return;
      const profiles = profilesForConnectorTarget(targetItems, target);
      const selectedProfileID = profileByTarget[connectorTargetKey(target)] || target.profile_id;
      const profileTarget = profiles.find((profile) => Number(profile.profile_id) === Number(selectedProfileID)) || profiles[0] || target;
      setSearchParams({ target: profileTarget.ref });
    },
    [profileByTarget, setSearchParams, targetItems],
  );

  const selectProfile = useCallback(
    (profileID) => {
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

  const toggleProject = useCallback((projectID) => {
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
    selectedRuntimeID: targetUsesLiveConsole(selectedTarget) ? String(selectedTarget.runtime_id || "") : "",
    selectedTarget,
    setSearch,
    targetItems,
    targetRows,
    toggleProject,
    unreadMessages,
  };
}
