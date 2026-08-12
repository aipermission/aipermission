import { ChevronDown, ChevronRight, Circle, Database, FolderKanban, PanelLeftClose, PanelLeftOpen } from "lucide-react";
import { connectorTargetKey, profilesForConnectorTarget } from "../../lib/connector-permissions";
import { ConnectorIcon } from "../../connectors/templates/common";
import { getConnectorModel } from "../../connectors/templates/registry";
import { CountBadge } from "../ui/badge";
import { Button } from "../ui/button";
import { Notice } from "../ui/notice";
import { emptySession, latestSessionForRuntime } from "./helpers";

export function ConsoleTargetSidebar({
  compact,
  onCompactChange,
  targetRows,
  search,
  onSearch,
  groups,
  collapsedProjects,
  onToggleProject,
  targetItems,
  liveConsoleTargets,
  sessions,
  selectedTarget,
  pendingConnectorApprovals,
  connectorActionApprovals,
  unreadMessages,
  onSelect,
  targetsState,
  targetsError,
  filteredTargetCount,
}) {
  return (
    <aside className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] overflow-hidden rounded-lg border border-stone-200 bg-white">
      <div className={`border-b border-stone-200 ${compact ? "grid gap-2 p-2" : "flex items-center justify-between gap-3 px-4 py-3"}`}>
        {compact ? (
          <Button type="button" variant="ghost" className="h-9 w-9 px-0" title="Expand connectors" onClick={() => onCompactChange(false)}>
            <PanelLeftOpen className="h-4 w-4" />
          </Button>
        ) : (
          <>
            <h3 className="flex items-center gap-2 text-sm font-semibold">
              <Database className="h-4 w-4" />
              Connectors
              <span className="rounded-full bg-stone-100 px-2 py-0.5 text-xs font-medium text-stone-500">{targetRows.length}</span>
            </h3>
            <Button
              type="button"
              variant="ghost"
              className="h-9 w-9 px-0"
              title="Collapse connectors"
              onClick={() => onCompactChange(true)}
            >
              <PanelLeftClose className="h-4 w-4" />
            </Button>
          </>
        )}
      </div>
      <div className="grid content-start gap-1 overflow-auto p-2">
        {!compact ? (
          <input
            className="mb-2 h-9 rounded-md border border-stone-200 bg-white px-3 text-sm text-stone-800 outline-none placeholder:text-stone-400 focus:border-emerald-500"
            placeholder="Search connectors"
            value={search}
            onChange={(event) => onSearch(event.target.value)}
          />
        ) : null}
        {groups.map((group) => (
          <ConsoleProjectGroup
            key={group.id}
            group={group}
            collapsed={Boolean(collapsedProjects[group.id])}
            compact={compact}
            onToggle={() => onToggleProject(group.id)}
          >
            {group.targets.map((target) => (
              <TargetListItem
                key={connectorTargetKey(target)}
                target={target}
                profileTargets={profilesForConnectorTarget(targetItems, target)}
                liveConsoleTargets={liveConsoleTargets}
                sessions={sessions}
                selectedTarget={selectedTarget}
                compact={compact}
                pendingConnectorApprovals={pendingConnectorApprovals}
                connectorActionApprovals={connectorActionApprovals}
                unreadMessages={unreadMessages}
                onSelect={onSelect}
              />
            ))}
          </ConsoleProjectGroup>
        ))}
        {targetsState === "ready" && targetItems.length === 0 && !compact ? <Notice>No targets yet.</Notice> : null}
        {targetsState === "ready" && targetRows.length > 0 && filteredTargetCount === 0 && !compact ? (
          <Notice>No connectors match that search.</Notice>
        ) : null}
        {targetsState === "error" && !compact ? <Notice tone="bad">{targetsError}</Notice> : null}
      </div>
    </aside>
  );
}

function ConsoleProjectGroup({ group, collapsed, compact, onToggle, children }) {
  return (
    <div className="grid gap-1">
      <button
        type="button"
        className={`${compact ? "grid h-8 w-10 place-items-center" : "flex h-8 items-center gap-2 px-2"} rounded-md text-left text-xs font-semibold text-stone-500 hover:bg-stone-100`}
        title={`${group.name} (${group.targets.length})`}
        onClick={onToggle}
      >
        {compact ? (
          <FolderKanban className="h-4 w-4" />
        ) : (
          <>
            {collapsed ? <ChevronRight className="h-3.5 w-3.5" /> : <ChevronDown className="h-3.5 w-3.5" />}
            <FolderKanban className="h-3.5 w-3.5" />
            <span className="min-w-0 flex-1 truncate">{group.name}</span>
            <span className="shrink-0 text-[10px]">{group.targets.length}</span>
          </>
        )}
      </button>
      {!collapsed ? children : null}
    </div>
  );
}

function TargetListItem({
  target,
  profileTargets,
  liveConsoleTargets,
  sessions,
  selectedTarget,
  compact,
  pendingConnectorApprovals,
  connectorActionApprovals,
  unreadMessages,
  onSelect,
}) {
  const runtimeID = targetUsesLiveConsole(target) ? target.runtime_id : null;
  const runtimeTarget = runtimeID ? liveConsoleTargets.data.find((item) => Number(item.id) === Number(runtimeID)) : null;
  const session = runtimeID ? latestSessionForRuntime(sessions, runtimeID) || emptySession : emptySession;
  const active = selectedTarget && connectorTargetKey(selectedTarget) === connectorTargetKey(target);
  const profiles = profileTargets?.length ? profileTargets : [target];
  const refs = new Set(profiles.map((profile) => profile.ref));
  const runtimeIDs = new Set(
    profiles
      .map((profile) => profile.runtime_id)
      .filter(Boolean)
      .map(Number),
  );
  const pendingCount = pendingConnectorApprovals.filter((approval) => refs.has(approval.target_ref)).length;
  const runningCount = connectorActionApprovals.data.filter(
    (approval) => approval.status === "running" && refs.has(approval.target_ref),
  ).length;
  const unreadCount = runtimeIDs.size > 0 ? unreadMessages.filter((message) => runtimeIDs.has(Number(message.runtime_id))).length : 0;
  const attentionCount = pendingCount + unreadCount;
  const status = selectedTargetStatus({ target, session, pendingCount, runningCount });
  const profileLabel = targetProfileLabel(target);
  const badgeClass = active ? "border-emerald-700 bg-emerald-900/70 text-emerald-50" : "border-stone-200 bg-stone-50 text-stone-500";

  return (
    <button
      type="button"
      title={`${targetDisplayName(target)} ${targetSubtitle(target, runtimeTarget)}`}
      className={`${compact ? "grid h-10 w-10 place-items-center px-0 py-0" : "grid gap-1.5 px-3 py-2 text-left"} rounded-md transition ${
        active ? "bg-emerald-950 text-white" : "text-stone-700 hover:bg-stone-100"
      }`}
      onClick={() => onSelect(target)}
    >
      {compact ? (
        <span className="relative grid h-full w-full place-items-center">
          <ConnectorIcon kind={target.connector_kind} className="h-4 w-4" />
          {attentionCount > 0 ? <CountBadge className="absolute -right-1 -top-1">{attentionCount}</CountBadge> : null}
          <ConsoleStatusDot status={status} className="absolute right-1 top-1 h-2.5 w-2.5" />
        </span>
      ) : (
        <>
          <span className="flex min-w-0 items-center justify-between gap-2">
            <span className="flex min-w-0 items-center gap-2">
              <ConnectorIcon
                kind={target.connector_kind}
                className={`h-3.5 w-3.5 shrink-0 ${active ? "text-emerald-100" : "text-stone-400"}`}
              />
              <span className="truncate text-sm font-semibold">{targetDisplayName(target)}</span>
            </span>
            <span className="flex shrink-0 items-center gap-1.5">
              {attentionCount > 0 ? <CountBadge>{attentionCount}</CountBadge> : null}
              <ConsoleStatusDot status={status} className={active && status === "offline" ? "text-red-200" : ""} />
            </span>
          </span>
          <span className={`truncate text-xs ${active ? "text-emerald-100" : "text-stone-500"}`}>
            {targetSubtitle(target, runtimeTarget)}
          </span>
          <span className="flex min-w-0 gap-1.5">
            <span className={`rounded-full border px-2 py-0.5 text-[10px] font-semibold uppercase ${badgeClass}`}>
              {target.connector_kind}
            </span>
            <span className={`truncate rounded-full border px-2 py-0.5 text-[10px] font-semibold ${badgeClass}`}>{profileLabel}</span>
            {profileTargets?.length > 1 ? (
              <span className={`rounded-full border px-2 py-0.5 text-[10px] font-semibold ${badgeClass}`}>
                {profileTargets.length} profiles
              </span>
            ) : null}
          </span>
        </>
      )}
    </button>
  );
}

export function consoleTargetRows(targets, selectedTarget, selectedProfileByTarget = {}) {
  const rows = [];
  const byKey = new Map();
  for (const target of targets) {
    const key = connectorTargetKey(target);
    if (!byKey.has(key)) {
      byKey.set(key, []);
      rows.push({ key, first: target });
    }
    byKey.get(key).push(target);
  }
  return rows.map(({ key, first }) => {
    const profiles = byKey.get(key) || [first];
    if (selectedTarget && connectorTargetKey(selectedTarget) === key) return selectedTarget;
    const preferredID = selectedProfileByTarget[key];
    return profiles.find((profile) => Number(profile.profile_id) === Number(preferredID)) || profiles[0] || first;
  });
}

export function groupConsoleTargetsByProject(targets) {
  const groups = new Map();
  for (const target of targets) {
    const id = String(target.project_id || "ungrouped");
    if (!groups.has(id)) groups.set(id, { id, name: target.project_name || "Ungrouped", targets: [] });
    groups.get(id).targets.push(target);
  }
  return [...groups.values()];
}

export function defaultConsoleTargetRef(targets, unreadMessages, pendingConnectorApprovals) {
  if (!targets.length) return "";
  const pendingConnector = pendingConnectorApprovals.find((approval) => targets.some((target) => target.ref === approval.target_ref));
  if (pendingConnector) return pendingConnector.target_ref;
  const unread = unreadMessages.find((message) =>
    targets.some((target) => target.runtime_id && Number(target.runtime_id) === Number(message.runtime_id)),
  );
  if (unread) {
    const target = targets.find((item) => item.runtime_id && Number(item.runtime_id) === Number(unread.runtime_id));
    if (target) return target.ref;
  }
  return targets[0].ref;
}

export function targetDisplayName(target) {
  if (!target) return "Target";
  return (
    getConnectorModel(target.connector_kind)?.targetDisplayName?.({ target }) || target.target_name || target.name || target.ref || "Target"
  );
}

export function targetSubtitle(target, runtimeTarget) {
  if (!target) return "";
  return (
    getConnectorModel(target.connector_kind)?.targetSubtitle?.({ target, runtimeTarget }) ||
    `${target.connector_kind} profile ${target.profile_label || "default"}`
  );
}

export function targetProfileLabel(target) {
  if (!target) return "default";
  return getConnectorModel(target.connector_kind)?.targetProfileLabel?.({ target }) || target.profile_label || "default";
}

export function targetUsesLiveConsole(target) {
  if (!target) return false;
  return Boolean(getConnectorModel(target.connector_kind)?.usesLiveConsole?.({ target }));
}

export function recoverableRunningActions(target) {
  if (!target) return [];
  const actions = getConnectorModel(target.connector_kind)?.recoverableRunningActions?.({ target });
  return Array.isArray(actions) ? actions.filter(Boolean).map(String) : [];
}

export function selectedTargetStatus({ target, session, pendingCount = 0, runningCount = 0 }) {
  if (pendingCount > 0 || runningCount > 0) return "busy";
  if (target?.connector_kind && !targetUsesLiveConsole(target)) return "idle";
  if (session?.status === "connected" || session?.status === "connecting") return "idle";
  return "offline";
}

export function ConsoleStatusDot({ status, className = "" }) {
  const colors = {
    offline: "fill-red-500 text-red-500",
    idle: "fill-emerald-500 text-emerald-500",
    busy: "fill-amber-400 text-amber-400",
  };
  const labels = {
    offline: "No live session",
    idle: "Target ready",
    busy: "Pending or running work",
  };
  const title = labels[status] || labels.offline;
  return <Circle className={`h-3 w-3 shrink-0 ${colors[status] || colors.offline} ${className}`} aria-label={title} title={title} />;
}
