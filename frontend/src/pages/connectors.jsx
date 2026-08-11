import {
  ChevronDown,
  ChevronRight,
  CircleCheck,
  CircleX,
  Edit3,
  FolderKanban,
  Plus,
  PlugZap,
  RefreshCcw,
  Search,
  Trash2,
} from "lucide-react";
import { useEffect, useEffectEvent, useMemo, useState } from "react";
import { apiGet } from "../lib/api";
import { useGateway } from "../lib/gateway-context";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Dialog } from "../components/ui/dialog";
import { Drawer } from "../components/ui/drawer";
import { Field, Input, Select } from "../components/ui/form";
import { Notice } from "../components/ui/notice";
import {
  ConnectorIcon,
  ConnectorKindCell,
  StatusCell,
  TargetCell,
  connectorKindLabel,
  connectorSummary,
} from "../connectors/templates/common";
import { supportedConnectorKinds } from "../connectors/templates/catalog";
import { ConnectorTemplateNotFound, getConnectorModel, getConnectorTemplate } from "../connectors/templates/registry";
import { useConnectorEditor } from "../connectors/editor/use-connector-editor";
import { connectorTestKey, useConnectorConnectionTests } from "../connectors/editor/use-connector-connection-tests";

function emptyConnectorForm(kind, options = {}) {
  return getConnectorModel(kind)?.emptyForm?.(options) || { connector_kind: kind };
}

export function ConnectorsPage() {
  const { targets: unifiedTargets, credentials, loadTargets: loadUnifiedTargets } = useGateway();
  const [catalog, setCatalog] = useState({ state: "loading", data: [], details: {}, error: null });
  const [targets, setTargets] = useState({ state: "loading", data: [], error: null });
  const [projects, setProjects] = useState({ state: "loading", data: [], error: null });
  const defaultConnectorKind = supportedConnectorKinds[0] || "";
  const [connectorOperation, setConnectorOperation] = useState({ open: false, connector_kind: "", type: "", state: "idle", error: null });
  const [addMenuOpen, setAddMenuOpen] = useState(false);
  const [profileSelections, setProfileSelections] = useState({});
  const [toast, setToast] = useState("");
  const [connectorSearch, setConnectorSearch] = useState("");
  const [collapsedProjects, setCollapsedProjects] = useState({});
  const catalogWarnings = useMemo(() => connectorCatalogWarnings(catalog), [catalog]);
  const availableConnectorKinds = useMemo(() => {
    if (catalog.state !== "ready") return [];
    const backendKinds = new Set(catalog.data.map((item) => item.kind));
    return supportedConnectorKinds.filter((kind) => backendKinds.has(kind) && catalog.details[kind]);
  }, [catalog]);

  const firstCredentialID = useMemo(() => (credentials.data[0] ? String(credentials.data[0].id) : ""), [credentials.data]);
  const defaultProjectID = useMemo(
    () => projects.data.find((project) => project.slug === "ungrouped")?.id || projects.data[0]?.id || "",
    [projects.data],
  );
  const editor = useConnectorEditor({
    defaultKind: defaultConnectorKind,
    firstCredentialID,
    defaultProjectID,
    emptyFormForKind: emptyConnectorForm,
    modelForKind: getConnectorModel,
    onRefresh: refreshConnectors,
    onOperation: openConnectorOperation,
  });
  const connectionTests = useConnectorConnectionTests({ modelForKind: getConnectorModel, onOperation: openConnectorOperation });
  const { drawer, deleteDialog, form, actionState: state } = editor;
  const tests = connectionTests.tests;
  const activeConnectorModel = getConnectorModel(form.connector_kind);
  const activeCredential = useMemo(
    () => activeConnectorModel?.activeCredential?.({ credentials: credentials.data, form }) || null,
    [activeConnectorModel, credentials.data, form],
  );
  const ActiveConnectorFormTemplate = getConnectorTemplate(form.connector_kind)?.Form || null;
  const connectorOptions = useMemo(
    () =>
      availableConnectorKinds.map((kind) => {
        const item = catalog.data.find((entry) => entry.kind === kind);
        return { kind, label: item?.label || connectorKindLabel(kind) };
      }),
    [availableConnectorKinds, catalog.data],
  );
  const refreshConnectorsForEffect = useEffectEvent(() => refreshConnectors());
  const reconcileProfileSelections = useEffectEvent(() => {
    setProfileSelections((current) => {
      const next = {};
      for (const target of targets.data || []) {
        const key = targetProfileSelectionKey(target);
        const profiles = target.profiles || [];
        const currentID = current[key];
        if (profiles.length === 0) continue;
        next[key] = profiles.some((profile) => String(profile.id) === String(currentID)) ? String(currentID) : String(profiles[0].id);
      }
      return next;
    });
  });
  const targetProfileSignature = targets.data
    .map((target) => `${target.connector_kind}:${target.id}:${(target.profiles || []).map((profile) => profile.id).join(",")}`)
    .join("|");

  useEffect(() => {
    void loadCatalog();
    void refreshConnectorsForEffect();
  }, []);

  useEffect(() => {
    reconcileProfileSelections();
  }, [targetProfileSignature]);

  async function refreshConnectors() {
    await Promise.all([loadTargets(), loadProjects(), loadUnifiedTargets()]);
  }

  async function loadProjects() {
    setProjects((current) => ({ ...current, state: "loading", error: null }));
    try {
      const data = await apiGet("/api/projects");
      setProjects({ state: "ready", data: data.items || [], error: null });
    } catch (error) {
      setProjects({ state: "error", data: [], error: error.message });
    }
  }

  async function loadCatalog() {
    try {
      const data = await apiGet("/api/connectors");
      const details = {};
      const detailFailures = [];
      await Promise.allSettled(
        (data.items || []).map(async (item) => {
          try {
            details[item.kind] = await apiGet(`/api/connectors/${item.kind}`);
          } catch (error) {
            detailFailures.push({ kind: item.kind, error: error.message || "failed to load connector details" });
          }
        }),
      );
      setCatalog({ state: "ready", data: data.items || [], details, detailFailures, error: null });
    } catch (error) {
      setCatalog({ state: "error", data: [], details: {}, detailFailures: [], error: error.message });
    }
  }

  async function loadTargets() {
    setTargets((current) => ({ ...current, state: "loading", error: null }));
    try {
      const data = await apiGet("/api/connector-targets/inventory");
      setTargets({ state: "ready", data: data.items || [], error: null });
    } catch (error) {
      setTargets({ state: "error", data: [], error: error.message });
    }
  }

  function showUnderConstruction(label) {
    setToast(`${label} is under construction.`);
    window.setTimeout(() => setToast(""), 2200);
  }

  function selectProfile(target, profileID) {
    setProfileSelections((current) => ({ ...current, [targetProfileSelectionKey(target)]: String(profileID || "") }));
  }

  function openConnectorOperation(operation) {
    if (!operation?.open || !operation?.connector_kind) return false;
    setConnectorOperation(operation);
    return true;
  }

  async function completeConnectorOperation(result, operation) {
    if (result?.testKey) {
      connectionTests.applyOperationResult(result.testKey, result.test);
      return;
    }
    editor.completeOperation(result, operation);
    await refreshConnectors();
  }

  return (
    <section className="mx-auto grid w-full max-w-7xl gap-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h3 className="text-lg font-semibold">Connectors</h3>
          <p className="text-sm text-stone-500">
            Create connector targets, attach credential profiles, then grant token permissions per action.
          </p>
        </div>
        <div className="flex gap-2">
          <Button type="button" variant="outline" onClick={refreshConnectors} disabled={targets.state === "loading"}>
            <RefreshCcw className="h-4 w-4" />
            Refresh
          </Button>
          <div className="relative">
            <Button type="button" onClick={() => setAddMenuOpen((current) => !current)}>
              <Plus className="h-4 w-4" />
              Add connector
              <ChevronDown className="h-4 w-4" />
            </Button>
            {addMenuOpen ? (
              <AddConnectorMenu
                catalog={catalog}
                onAdd={(kind) => {
                  setAddMenuOpen(false);
                  editor.openCreate(kind);
                }}
              />
            ) : null}
          </div>
        </div>
      </div>

      {catalog.state === "error" ? <Notice tone="bad">{catalog.error}</Notice> : null}
      {catalogWarnings.map((warning) => (
        <Notice tone="warn" key={warning}>
          {warning}
        </Notice>
      ))}
      {targets.state === "error" ? <Notice tone="bad">{targets.error}</Notice> : null}
      {projects.state === "error" ? <Notice tone="bad">{projects.error}</Notice> : null}
      {state.message ? <Notice tone="good">{state.message}</Notice> : null}
      {state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null}
      {toast ? (
        <div className="fixed right-5 top-5 z-[80] rounded-md border border-stone-700 bg-stone-950 px-4 py-3 text-sm font-semibold text-white shadow-xl">
          {toast}
        </div>
      ) : null}

      <ConnectorTargetsTable
        targets={targets}
        projects={projects.data}
        search={connectorSearch}
        collapsedProjects={collapsedProjects}
        onSearch={setConnectorSearch}
        onToggleProject={(projectID) => setCollapsedProjects((current) => ({ ...current, [projectID]: !current[projectID] }))}
        catalog={catalog}
        unifiedTargets={unifiedTargets.data}
        credentials={credentials.data}
        profileSelections={profileSelections}
        tests={tests}
        onSelectProfile={selectProfile}
        onTestConnector={connectionTests.run}
        onOperation={setConnectorOperation}
        onUnderConstruction={showUnderConstruction}
        onEdit={editor.openEdit}
        onDelete={editor.requestDelete}
      />

      <Drawer
        open={drawer.open}
        title={drawer.mode === "edit" ? `Edit ${drawer.target?.name || "connector"}` : "Add connector"}
        description={
          drawer.mode === "edit"
            ? "Update this connector and its default credential profile."
            : "Choose a connector type, then create its first default credential profile."
        }
        onClose={editor.closeEditor}
      >
        <form className="grid gap-4" onSubmit={editor.save}>
          {drawer.mode === "create" ? (
            <Field>
              Connector type
              <Select value={form.connector_kind} onChange={(event) => editor.selectKind(event.target.value)}>
                {connectorOptions.map((option) => (
                  <option value={option.kind} key={option.kind}>
                    {option.label}
                  </option>
                ))}
              </Select>
            </Field>
          ) : null}
          <Field>
            Project
            <Select value={form.project_id || ""} onChange={(event) => editor.updateField("project_id", event.target.value)} required>
              <option value="" disabled>
                Select project
              </option>
              {projects.data.map((project) => (
                <option value={project.id} key={project.id}>
                  {project.name}
                </option>
              ))}
            </Select>
          </Field>
          {ActiveConnectorFormTemplate ? (
            <ActiveConnectorFormTemplate
              form={form}
              mode={drawer.mode}
              credentials={credentials.data}
              targets={targets.data.filter((target) => String(target.project_id || "") === String(form.project_id || ""))}
              activeCredential={activeCredential}
              onChange={editor.updateField}
            />
          ) : (
            <ConnectorTemplateNotFound kind={form.connector_kind} slot="form" />
          )}
          {state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null}
          <div className="grid gap-2 sm:grid-cols-2">
            <Button type="button" variant="outline" onClick={editor.closeEditor}>
              Cancel
            </Button>
            <Button
              type="submit"
              disabled={
                activeConnectorModel?.submitDisabled?.({ state, mode: drawer.mode, form, credentials: credentials.data }) ??
                state.state === "saving"
              }
            >
              {activeConnectorModel?.submitLabel?.({ state, mode: drawer.mode, form }) ||
                (drawer.mode === "edit" ? "Save changes" : "Create connector")}
            </Button>
          </div>
        </form>
      </Drawer>

      {availableConnectorKinds.map((kind) => {
        const OperationsTemplate = getConnectorTemplate(kind)?.Operations;
        return OperationsTemplate ? (
          <OperationsTemplate
            key={kind}
            value={connectorOperation}
            credentials={credentials.data}
            onChange={setConnectorOperation}
            onOperationComplete={completeConnectorOperation}
          />
        ) : null;
      })}
      <DeleteConnectorDialog value={deleteDialog} state={state} onDelete={editor.remove} onClose={editor.closeDelete} />
    </section>
  );
}

function ConnectorTargetsTable({
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
  const query = search.trim().toLowerCase();
  const groups = projects
    .map((project) => {
      const projectMatches =
        query &&
        [project.name, project.slug].some((value) =>
          String(value || "")
            .toLowerCase()
            .includes(query),
        );
      return {
        project,
        targets: targets.data.filter(
          (target) =>
            target.project_id === project.id &&
            (!query ||
              projectMatches ||
              [target.name, target.connector_kind, ...(target.profiles || []).map((profile) => profile.label)].some((value) =>
                String(value || "")
                  .toLowerCase()
                  .includes(query),
              )),
        ),
      };
    })
    .filter((group) => group.targets.length > 0 || (!query && group.project.target_count > 0));
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
      {targets.state === "loading" ? (
        <div className="p-4">
          <Notice>Loading connectors...</Notice>
        </div>
      ) : null}
      {targets.state === "ready" && targets.data.length === 0 ? (
        <div className="p-4">
          <Notice>
            Create your first connector target. Every connector uses the same target, credential profile, permission, history, and audit
            pipeline.
          </Notice>
        </div>
      ) : null}
      {targets.state === "ready" && targets.data.length > 0 && groups.length === 0 ? (
        <div className="p-4">
          <Notice>No connectors match that search.</Notice>
        </div>
      ) : null}
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

function targetProfileSelectionKey(target) {
  return `${target?.connector_kind || ""}:${target?.id || ""}`;
}

function ConnectorTargetRow(props) {
  const {
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
  } = props;
  const template = getConnectorTemplate(target.connector_kind);
  const model = template?.model;
  const RowActionsTemplate = template?.RowActions;
  const profile = selectedConnectorProfile(target, selectedProfileID);
  const profileRef = profile?.ref || "";
  const runtime = unifiedTargets.find((item) => item.ref === profileRef);
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
          <Button
            type="button"
            variant="outline"
            className="h-9 w-9 px-0"
            title="Test connection"
            disabled={!profile || test?.state === "testing" || test?.cooldown}
            onClick={() => onTestConnector(target, profile)}
          >
            <PlugZap className="h-4 w-4" />
          </Button>
          <Button
            type="button"
            variant="outline"
            className="h-9 w-9 px-0"
            title="Edit connector"
            disabled={!canEdit}
            onClick={() => canEdit && onEdit(target, profile)}
          >
            <Edit3 className="h-4 w-4" />
          </Button>
          <Button
            type="button"
            variant="outline"
            className="h-9 w-9 px-0"
            title="Delete connector"
            disabled={!canDelete}
            onClick={() => canDelete && onDelete(target)}
          >
            <Trash2 className="h-4 w-4" />
          </Button>
        </div>
      </td>
    </tr>
  );
}

function selectedConnectorProfile(target, selectedProfileID) {
  const profiles = target?.profiles || [];
  if (profiles.length === 0) return null;
  return profiles.find((profile) => String(profile.id) === String(selectedProfileID)) || profiles[0];
}

function ConnectorProfilesCell({ target, selectedProfileID, onSelectProfile }) {
  const profiles = target.profiles || [];
  if (profiles.length === 0) {
    return <span className="text-xs text-stone-500">No profiles</span>;
  }
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

function AddConnectorMenu({ catalog, onAdd }) {
  const backendKinds = new Set(catalog.data.map((item) => item.kind));
  const availableKinds = supportedConnectorKinds.filter((kind) => backendKinds.has(kind) && catalog.details[kind]);
  return (
    <div className="absolute right-0 top-12 z-40 w-[360px] overflow-hidden rounded-lg border border-stone-200 bg-white p-2 shadow-xl dark-panel">
      <div className="grid gap-1">
        {availableKinds.map((kind) => {
          const detail = catalog.details[kind];
          return (
            <button
              type="button"
              key={kind}
              className="grid gap-2 rounded-md px-3 py-3 text-left transition hover:bg-stone-50 focus:bg-stone-50 focus:outline-none dark-panel-subtle"
              onClick={() => onAdd(kind)}
            >
              <span className="flex items-center justify-between gap-3">
                <span className="inline-flex min-w-0 items-center gap-2 font-semibold text-stone-900">
                  <ConnectorIcon kind={kind} className="h-4 w-4 shrink-0 text-stone-500" />
                  <span className="truncate">{detail?.label || connectorKindLabel(kind)}</span>
                </span>
                <Badge tone="neutral">{detail?.version || "0.1"}</Badge>
              </span>
              <span className="text-xs leading-5 text-stone-500">{connectorSummary(kind)}</span>
            </button>
          );
        })}
        {catalog.state === "loading" ? <p className="px-3 py-2 text-sm text-stone-500">Loading connector catalog...</p> : null}
        {catalog.state === "ready" && availableKinds.length === 0 ? (
          <p className="px-3 py-2 text-sm text-stone-500">
            No connector type is available in both the backend catalog and frontend templates.
          </p>
        ) : null}
      </div>
    </div>
  );
}

function connectorCatalogWarnings(catalog) {
  if (catalog.state !== "ready") return [];
  const backendKinds = new Set(catalog.data.map((item) => item.kind));
  const frontendKinds = new Set(supportedConnectorKinds);
  const backendOnly = [...backendKinds].filter((kind) => !frontendKinds.has(kind)).sort();
  const frontendOnly = [...frontendKinds].filter((kind) => !backendKinds.has(kind)).sort();
  const warnings = [];
  if (backendOnly.length > 0) {
    warnings.push(`Backend connector catalog has no matching frontend template: ${backendOnly.join(", ")}.`);
  }
  if (frontendOnly.length > 0) {
    warnings.push(`Frontend connector template has no matching backend connector: ${frontendOnly.join(", ")}.`);
  }
  for (const failure of catalog.detailFailures || []) {
    warnings.push(`Backend connector detail failed for ${failure.kind}: ${failure.error}.`);
  }
  return warnings;
}

function DeleteConnectorDialog({ value, state, onDelete, onClose }) {
  const target = value.target;
  const dialog = target ? getConnectorModel(target.connector_kind)?.deleteDialog?.({ target }) : null;
  return (
    <Dialog
      open={value.open}
      title={dialog?.title || "Delete connector"}
      description={dialog?.description || "Remove this connector target from aipermission."}
      onClose={onClose}
      size="md"
    >
      {target ? (
        <div className="grid gap-4">
          <div className="rounded-md border border-stone-200 bg-stone-50 p-3 text-sm text-stone-700">
            {(dialog?.details || [])
              .filter((item) => item.value)
              .map((item) => (
                <p className="mt-1 first:mt-0" key={item.label}>
                  <span className="font-semibold">{item.label}: </span>
                  <span className={item.label === "Reference" ? "font-mono text-xs" : ""}>{item.value}</span>
                </p>
              ))}
          </div>
          {dialog?.notice ? <Notice tone="warn">{dialog.notice}</Notice> : null}
          {state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null}
          <div className="grid gap-2 sm:grid-cols-2">
            {(
              dialog?.actions || [
                { label: "Cancel", action: "close", variant: "outline" },
                { label: "Delete connector", removeKey: false },
              ]
            ).map((action) => (
              <Button
                type="button"
                variant={action.variant || "default"}
                onClick={() => (action.action === "close" ? onClose() : onDelete(Boolean(action.removeKey)))}
                disabled={state.state === "deleting"}
                key={action.label}
              >
                {state.state === "deleting" && action.pendingLabel ? action.pendingLabel : action.label}
              </Button>
            ))}
          </div>
        </div>
      ) : null}
    </Dialog>
  );
}
