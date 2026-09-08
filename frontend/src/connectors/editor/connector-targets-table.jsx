import { ChevronDown, ChevronRight, CircleCheck, CircleX, Edit3, FolderKanban, PlugZap, Search, Trash2 } from "lucide-react";
import { Badge } from "../../components/ui/badge";
import { Button } from "../../components/ui/button";
import { Input, Select } from "../../components/ui/form";
import { Notice } from "../../components/ui/notice";
import { ConnectorKindCell, StatusCell, TargetCell } from "../templates/common";
import { ConnectorTemplateNotFound, getConnectorTemplate } from "../templates/registry";
import { connectorTargetGroups } from "./connector-target-groups";
import { connectorTestKey } from "./use-connector-connection-tests";
import { targetProfileSelectionKey } from "./use-connector-inventory";

export function ConnectorTargetsTable({
  targets,
  projects,
  search,
  collapsedProjects,
  onSearch,
  onToggleProject,
  catalog,
  unifiedTargets,
  credentials,
  profileSelections,
  tests,
  onSelectProfile,
  onTestConnector,
  onOperation,
  onUnderConstruction,
  onEdit,
  onDelete,
}) {
  const groups = connectorTargetGroups(projects, targets.data, search);
  return (
    <div className="overflow-hidden rounded-lg border border-stone-200 bg-white">
      <div className="border-b border-stone-200 bg-stone-50 p-3">
        <div className="relative max-w-md">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-stone-400" />
          <Input value={search} onChange={(event) => onSearch(event.target.value)} placeholder="Search connectors" className="pl-9" />
        </div>
      </div>
      <table className="w-full table-fixed border-collapse text-left text-sm">
        <thead className="bg-stone-50 text-xs uppercase text-stone-500">
          <tr>
            <th className="w-[18%] px-4 py-3 font-semibold">Connector</th>
            <th className="w-[24%] px-4 py-3 font-semibold">Target</th>
            <th className="w-[19%] px-4 py-3 font-semibold">Profiles</th>
            <th className="w-[11%] px-4 py-3 font-semibold">Status</th>
            <th className="w-[14%] px-4 py-3 text-right font-semibold">Operations</th>
            <th className="w-[14%] px-4 py-3 text-right font-semibold">Actions</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-stone-200">
          {groups.map(({ project, targets: projectTargets }) => (
            <ProjectTargetRows
              key={project.id}
              project={project}
              targets={projectTargets}
              collapsed={Boolean(collapsedProjects[project.id])}
              onToggle={() => onToggleProject(project.id)}
              catalog={catalog}
              unifiedTargets={unifiedTargets}
              credentials={credentials}
              profileSelections={profileSelections}
              tests={tests}
              onSelectProfile={onSelectProfile}
              onTestConnector={onTestConnector}
              onOperation={onOperation}
              onUnderConstruction={onUnderConstruction}
              onEdit={onEdit}
              onDelete={onDelete}
            />
          ))}
        </tbody>
      </table>
      <ConnectorTableState targets={targets} groupCount={groups.length} />
    </div>
  );
}

function ProjectTargetRows({ project, targets, collapsed, onToggle, ...rowProps }) {
  return (
    <>
      <tr className="bg-stone-50">
        <td colSpan={6} className="px-3 py-2">
          <button
            type="button"
            className="flex w-full items-center gap-2 text-left text-xs font-semibold uppercase text-stone-600"
            onClick={onToggle}
          >
            {collapsed ? <ChevronRight className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
            <FolderKanban className="h-4 w-4" />
            <span className="truncate">{project.name}</span>
            <Badge tone="neutral">{targets.length}</Badge>
          </button>
        </td>
      </tr>
      {!collapsed
        ? targets.map((target) => (
            <ConnectorTargetRow
              key={`${target.connector_kind}:${target.id}`}
              target={target}
              selectedProfileID={rowProps.profileSelections[targetProfileSelectionKey(target)] || ""}
              {...rowProps}
            />
          ))
        : null}
    </>
  );
}

function ConnectorTargetRow({
  target,
  catalog,
  unifiedTargets,
  credentials,
  selectedProfileID,
  tests,
  onSelectProfile,
  onTestConnector,
  onOperation,
  onUnderConstruction,
  onEdit,
  onDelete,
}) {
  const template = getConnectorTemplate(target.connector_kind);
  const model = template?.model;
  const RowActionsTemplate = template?.RowActions;
  const profile = selectedConnectorProfile(target, selectedProfileID);
  const runtime = unifiedTargets.find((item) => item.ref === (profile?.ref || ""));
  const endpoint = model?.targetEndpoint?.({ target, profile, runtime }) || target.ref || `${target.connector_kind}:${target.id}`;
  const credentialHint = model?.credentialHint?.({ target, profile, credentials }) || null;
  const test = tests[connectorTestKey(target, profile)];
  const canEdit = Boolean(profile) && (model?.canEdit?.({ target, profile }) ?? true);
  const canDelete = model?.canDelete?.({ target }) ?? true;
  return (
    <tr className="align-top hover:bg-stone-50">
      <ConnectorKindCell target={target} catalog={catalog} />
      <TargetCell target={target} endpoint={endpoint} />
      <td className="px-4 py-4">
        <div className="grid gap-1.5">
          <ConnectorProfilesCell target={target} selectedProfileID={profile?.id || ""} onSelectProfile={onSelectProfile} />
          {credentialHint ? <span className="truncate text-xs text-stone-500">{credentialHint}</span> : null}
        </div>
      </td>
      <td className="px-4 py-4">
        <div className="grid gap-1.5">
          <StatusCell target={target} />
          <ConnectorTestState value={test} />
        </div>
      </td>
      <td className="px-4 py-4">
        <div className="flex justify-end gap-2">
          {RowActionsTemplate ? (
            <RowActionsTemplate target={target} profile={profile} onOperation={onOperation} onUnderConstruction={onUnderConstruction} />
          ) : (
            <ConnectorTemplateNotFound kind={target.connector_kind} slot="row-actions" />
          )}
        </div>
      </td>
      <td className="px-4 py-4">
        <div className="flex justify-end gap-2">
          <IconAction
            title="Test connection"
            disabled={!profile || test?.state === "testing" || test?.cooldown}
            onClick={() => onTestConnector(target, profile)}
          >
            <PlugZap className="h-4 w-4" />
          </IconAction>
          <IconAction title="Edit connector" disabled={!canEdit} onClick={() => canEdit && onEdit(target, profile)}>
            <Edit3 className="h-4 w-4" />
          </IconAction>
          <IconAction title="Delete connector" disabled={!canDelete} onClick={() => canDelete && onDelete(target)}>
            <Trash2 className="h-4 w-4" />
          </IconAction>
        </div>
      </td>
    </tr>
  );
}

function IconAction({ title, disabled, onClick, children }) {
  return (
    <Button type="button" variant="outline" className="h-9 w-9 px-0" title={title} disabled={disabled} onClick={onClick}>
      {children}
    </Button>
  );
}

function selectedConnectorProfile(target, selectedProfileID) {
  const profiles = target?.profiles || [];
  if (profiles.length === 0) return null;
  return profiles.find((profile) => String(profile.id) === String(selectedProfileID)) || profiles[0];
}

function ConnectorProfilesCell({ target, selectedProfileID, onSelectProfile }) {
  const profiles = target.profiles || [];
  if (profiles.length === 0) return <span className="text-xs text-stone-500">No profiles</span>;
  if (profiles.length === 1) {
    return (
      <Badge tone="neutral" title={profiles[0].ref}>
        {profiles[0].label}
      </Badge>
    );
  }
  return (
    <Select
      value={selectedProfileID ? String(selectedProfileID) : ""}
      onChange={(event) => onSelectProfile(target, event.target.value)}
      className="h-9 text-xs"
    >
      <option value="">Select profile</option>
      {profiles.map((profile) => (
        <option value={profile.id} key={profile.id}>
          {profile.label}
        </option>
      ))}
    </Select>
  );
}

function ConnectorTestState({ value }) {
  if (!value || value.state === "idle") return null;
  if (value.state === "testing") return <span className="text-xs text-stone-500">Testing...</span>;
  if (value.state === "ok") {
    return (
      <span className="flex items-center gap-1 text-xs text-emerald-800 dark-status-good">
        <CircleCheck className="h-3.5 w-3.5" />
        {value.data?.duration_ms || value.data?.durationMS || 0}ms
      </span>
    );
  }
  const error = value.error || value.data?.message || "Connection test failed";
  return (
    <span className="grid max-w-56 gap-0.5 text-xs text-red-800 dark-status-bad" title={error}>
      <span className="flex items-center gap-1 font-medium">
        <CircleX className="h-3.5 w-3.5 shrink-0" />
        Failed
      </span>
      <span className="line-clamp-2 leading-4">{error}</span>
    </span>
  );
}

function ConnectorTableState({ targets, groupCount }) {
  if (targets.state === "loading") return <TableNotice>Loading connectors...</TableNotice>;
  if (targets.state === "ready" && targets.data.length === 0) {
    return (
      <TableNotice>
        Create your first connector target. Every connector uses the same target, credential profile, permission, history, and audit
        pipeline.
      </TableNotice>
    );
  }
  if (targets.state === "ready" && targets.data.length > 0 && groupCount === 0)
    return <TableNotice>No connectors match that search.</TableNotice>;
  return null;
}

function TableNotice({ children }) {
  return (
    <div className="p-4">
      <Notice>{children}</Notice>
    </div>
  );
}
