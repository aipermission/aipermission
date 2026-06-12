import { ChevronDown, CircleCheck, CircleX, Edit3, Plus, PlugZap, RefreshCcw, Trash2 } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { apiGet } from "../lib/api";
import { useGateway } from "../lib/gateway-context";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Dialog } from "../components/ui/dialog";
import { Drawer } from "../components/ui/drawer";
import { Field, Select } from "../components/ui/form";
import { Notice } from "../components/ui/notice";
import { ConnectorIcon, ConnectorKindCell, ProfilesCell, StatusCell, TargetCell, connectorKindLabel, connectorSummary } from "../connectors/templates/common";
import { supportedConnectorKinds } from "../connectors/templates/catalog";
import { ConnectorTemplateNotFound, getConnectorModel, getConnectorTemplate } from "../connectors/templates/registry";

function emptyConnectorForm(kind, options = {}) {
  return getConnectorModel(kind)?.emptyForm?.(options) || { connector_kind: kind };
}

export function ConnectorsPage() {
  const { targets: unifiedTargets, credentials, loadTargets: loadUnifiedTargets } = useGateway();
  const [catalog, setCatalog] = useState({ state: "loading", data: [], details: {}, error: null });
  const [targets, setTargets] = useState({ state: "loading", data: [], error: null });
  const defaultConnectorKind = supportedConnectorKinds[0] || "";
  const [drawer, setDrawer] = useState({ open: false, mode: "create", kind: defaultConnectorKind, target: null });
  const [deleteDialog, setDeleteDialog] = useState({ open: false, target: null });
  const [form, setForm] = useState(() => emptyConnectorForm(defaultConnectorKind));
  const [state, setState] = useState({ state: "idle", error: null, message: "" });
  const [tests, setTests] = useState({});
  const [connectorOperation, setConnectorOperation] = useState({ open: false, connector_kind: "", type: "", state: "idle", error: null });
  const [addMenuOpen, setAddMenuOpen] = useState(false);
  const [toast, setToast] = useState("");

  const firstCredentialID = useMemo(() => (credentials.data[0] ? String(credentials.data[0].id) : ""), [credentials.data]);
  const activeConnectorModel = getConnectorModel(form.connector_kind);
  const activeCredential = useMemo(
    () => activeConnectorModel?.activeCredential?.({ credentials: credentials.data, form }) || null,
    [activeConnectorModel, credentials.data, form]
  );
  const ActiveConnectorFormTemplate = getConnectorTemplate(form.connector_kind)?.Form || null;
  const connectorOptions = useMemo(
    () =>
      supportedConnectorKinds.map((kind) => {
        const item = catalog.data.find((entry) => entry.kind === kind);
        return { kind, label: item?.label || connectorKindLabel(kind) };
      }),
    [catalog.data]
  );

  useEffect(() => {
    setForm((current) => getConnectorModel(current.connector_kind)?.syncForm?.({ form: current, firstCredentialID }) || current);
  }, [firstCredentialID]);

  useEffect(() => {
    void loadCatalog();
    void refreshConnectors();
  }, []);

  async function refreshConnectors() {
    await Promise.all([loadTargets(), loadUnifiedTargets()]);
  }

  async function loadCatalog() {
    try {
      const data = await apiGet("/api/connectors");
      const details = {};
      await Promise.all(
        (data.items || []).map(async (item) => {
          details[item.kind] = await apiGet(`/api/connectors/${item.kind}`);
        })
      );
      setCatalog({ state: "ready", data: data.items || [], details, error: null });
    } catch (error) {
      setCatalog({ state: "error", data: [], details: {}, error: error.message });
    }
  }

  async function loadTargets() {
    setTargets((current) => ({ ...current, state: "loading", error: null }));
    try {
      const data = await apiGet("/api/connector-targets");
      const items = await Promise.all((data.items || []).map((target) => apiGet(`/api/connector-targets/${target.id}`)));
      setTargets({ state: "ready", data: items, error: null });
    } catch (error) {
      setTargets({ state: "error", data: [], error: error.message });
    }
  }

  function openCreateDrawer(kind = defaultConnectorKind) {
    setState({ state: "idle", error: null, message: "" });
    setAddMenuOpen(false);
    setForm(emptyConnectorForm(kind, { firstCredentialID }));
    setDrawer({ open: true, mode: "create", kind, target: null });
  }

  function showUnderConstruction(label) {
    setToast(`${label} is under construction.`);
    window.setTimeout(() => setToast(""), 2200);
  }

  function openEditDrawer(target) {
    const model = getConnectorModel(target.connector_kind);
    if (!model?.formFromTarget) {
      setState({ state: "error", error: `Connector model not found for ${target.connector_kind}.`, message: "" });
      return;
    }
    setState({ state: "idle", error: null, message: "" });
    setForm(model.formFromTarget({ target }));
    setDrawer({ open: true, mode: "edit", kind: target.connector_kind, target });
  }

  function setConnectorKind(kind) {
    setState({ state: "idle", error: null, message: "" });
    setForm(emptyConnectorForm(kind, { firstCredentialID }));
    setDrawer((current) => ({ ...current, kind }));
  }

  function updateForm(field, value) {
    setForm((current) => ({ ...current, [field]: value }));
  }

  async function saveConnector(event) {
    event.preventDefault();
    const model = getConnectorModel(form.connector_kind);
    if (!model?.save) {
      setState({ state: "error", error: `Connector model not found for ${form.connector_kind}.`, message: "" });
      return;
    }
    setState({ state: "saving", error: null, message: "" });
    try {
      await model.save({ mode: drawer.mode, form, target: drawer.target });
      setDrawer({ open: false, mode: "create", kind: form.connector_kind, target: null });
      setForm(emptyConnectorForm(form.connector_kind, { firstCredentialID }));
      setState({ state: "idle", error: null, message: drawer.mode === "edit" ? "Connector updated." : "Connector created." });
      await refreshConnectors();
    } catch (error) {
      const action = model.hostKeyActionFromError?.(error, { mode: drawer.mode, form, target: drawer.target });
      if (action && openConnectorChallenge(error, action)) {
        setState({ state: "idle", error: null, message: "" });
        return;
      }
      setState({ state: "error", error: error.message, message: "" });
    }
  }

  async function deleteConnector(removeKey) {
    if (!deleteDialog.target) return;
    const model = getConnectorModel(deleteDialog.target.connector_kind);
    if (!model?.deleteTarget) {
      setState({ state: "error", error: `Connector model not found for ${deleteDialog.target.connector_kind}.`, message: "" });
      return;
    }
    setState({ state: "deleting", error: null, message: "" });
    try {
      await model.deleteTarget({ target: deleteDialog.target, removeKey });
      setDeleteDialog({ open: false, target: null });
      setState({ state: "idle", error: null, message: "Connector deleted." });
      await refreshConnectors();
    } catch (error) {
      setState({ state: "error", error: error.message, message: "" });
    }
  }

  function openConnectorChallenge(error, action) {
    if (!error.data?.host_key || !action?.kind) return false;
    setConnectorOperation({ open: true, connector_kind: action.kind, type: "host-key", hostKey: error.data.host_key, action, state: "idle", error: null });
    return true;
  }

  async function completeHostKeyAction(result, action) {
    if (result?.testKey) {
      setTests((current) => ({
        ...current,
        [result.testKey]: { state: result.test.ok ? "ok" : "error", error: result.test.error, data: result.test.data },
      }));
      return;
    }
    setDrawer({ open: false, mode: "create", kind: action.kind, target: null });
    setForm(emptyConnectorForm(action.kind, { firstCredentialID }));
    setState({ state: "idle", error: null, message: result?.message || "Connector updated." });
    await refreshConnectors();
  }

  async function testConnector(target) {
    const testKey = connectorTestKey(target);
    const model = getConnectorModel(target.connector_kind);
    if (!model?.test) {
      setTests((current) => ({ ...current, [testKey]: { state: "error", error: `Connector model not found for ${target.connector_kind}.`, data: null } }));
      return;
    }
    setTests((current) => ({ ...current, [testKey]: { state: "testing", error: null, data: null } }));
    try {
      const result = await model.test({ target });
      setTests((current) => ({ ...current, [testKey]: { state: result.ok ? "ok" : "error", error: result.error, data: result.data } }));
    } catch (error) {
      const action = model.hostKeyActionFromError?.(error, { operation: "test", target, testKey });
      if (action && openConnectorChallenge(error, action)) {
        setTests((current) => ({ ...current, [testKey]: { state: "idle", error: null, data: null } }));
        return;
      }
      setTests((current) => ({ ...current, [testKey]: { state: "error", error: error.message, data: null } }));
    }
  }

  return (
    <section className="mx-auto grid w-full max-w-7xl gap-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h3 className="text-lg font-semibold">Connectors</h3>
          <p className="text-sm text-stone-500">Create connector targets, attach credential profiles, then grant token permissions per action.</p>
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
            {addMenuOpen ? <AddConnectorMenu catalog={catalog} onAdd={openCreateDrawer} /> : null}
          </div>
        </div>
      </div>

      {catalog.state === "error" ? <Notice tone="bad">{catalog.error}</Notice> : null}
      {targets.state === "error" ? <Notice tone="bad">{targets.error}</Notice> : null}
      {state.message ? <Notice tone="good">{state.message}</Notice> : null}
      {state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null}
      {toast ? <div className="fixed right-5 top-5 z-[80] rounded-md border border-stone-700 bg-stone-950 px-4 py-3 text-sm font-semibold text-white shadow-xl">{toast}</div> : null}

      <ConnectorTargetsTable
        targets={targets}
        catalog={catalog}
        unifiedTargets={unifiedTargets.data}
        credentials={credentials.data}
        tests={tests}
        onTestConnector={testConnector}
        onOperation={setConnectorOperation}
        onUnderConstruction={showUnderConstruction}
        onEdit={openEditDrawer}
        onDelete={(target) => setDeleteDialog({ open: true, target })}
      />

      <Drawer
        open={drawer.open}
        title={drawer.mode === "edit" ? `Edit ${drawer.target?.name || "connector"}` : "Add connector"}
        description={drawer.mode === "edit" ? "Update this connector and its default credential profile." : "Choose a connector type, then create its first default credential profile."}
        onClose={() => setDrawer({ open: false, mode: "create", kind: defaultConnectorKind, target: null })}
      >
        <form className="grid gap-4" onSubmit={saveConnector}>
          {drawer.mode === "create" ? (
            <Field>
              Connector type
              <Select value={form.connector_kind} onChange={(event) => setConnectorKind(event.target.value)}>
                {connectorOptions.map((option) => (
                  <option value={option.kind} key={option.kind}>
                    {option.label}
                  </option>
                ))}
              </Select>
            </Field>
          ) : null}
          {ActiveConnectorFormTemplate ? (
            <ActiveConnectorFormTemplate
              form={form}
              mode={drawer.mode}
              credentials={credentials.data}
              activeCredential={activeCredential}
              onChange={updateForm}
            />
          ) : (
            <ConnectorTemplateNotFound kind={form.connector_kind} slot="form" />
          )}
          {state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null}
          <div className="grid gap-2 sm:grid-cols-2">
            <Button type="button" variant="outline" onClick={() => setDrawer({ open: false, mode: "create", kind: defaultConnectorKind, target: null })}>
              Cancel
            </Button>
            <Button type="submit" disabled={activeConnectorModel?.submitDisabled?.({ state, mode: drawer.mode, form, credentials: credentials.data }) ?? state.state === "saving"}>
              {activeConnectorModel?.submitLabel?.({ state, mode: drawer.mode, form }) || (drawer.mode === "edit" ? "Save changes" : "Create connector")}
            </Button>
          </div>
        </form>
      </Drawer>

      {supportedConnectorKinds.map((kind) => {
        const OperationsTemplate = getConnectorTemplate(kind)?.Operations;
        return OperationsTemplate ? (
          <OperationsTemplate
            key={kind}
            value={connectorOperation}
            credentials={credentials.data}
            onChange={setConnectorOperation}
            onHostKeyActionComplete={completeHostKeyAction}
          />
        ) : null;
      })}
      <DeleteConnectorDialog
        value={deleteDialog}
        state={state}
        onDelete={deleteConnector}
        onClose={() => setDeleteDialog({ open: false, target: null })}
      />
    </section>
  );
}

function ConnectorTargetsTable({ targets, catalog, unifiedTargets, credentials, tests, onTestConnector, onOperation, onUnderConstruction, onEdit, onDelete }) {
  return (
    <div className="overflow-hidden rounded-lg border border-stone-200 bg-white">
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
          {targets.data.map((target) => (
            <ConnectorTargetRow
              key={`${target.connector_kind}:${target.id}`}
              target={target}
              catalog={catalog}
              unifiedTargets={unifiedTargets}
              credentials={credentials}
              tests={tests}
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
          <Notice>Create your first connector target. Every connector uses the same target, credential profile, permission, history, and audit pipeline.</Notice>
        </div>
      ) : null}
    </div>
  );
}

function connectorTestKey(target) {
  const profileID = target.profiles?.[0]?.id || "target";
  return `${target.connector_kind}:${target.id}:${profileID}`;
}

function ConnectorTargetRow(props) {
  const { target, catalog, unifiedTargets, credentials, tests, onTestConnector, onOperation, onUnderConstruction, onEdit, onDelete } = props;
  const template = getConnectorTemplate(target.connector_kind);
  const model = template?.model;
  const RowActionsTemplate = template?.RowActions;
  const profileRef = target.profiles?.[0]?.ref || "";
  const runtime = unifiedTargets.find((item) => item.ref === profileRef);
  const endpoint = model?.targetEndpoint?.({ target, runtime }) || target.ref || `${target.connector_kind}:${target.id}`;
  const credentialHint = model?.credentialHint?.({ target, credentials }) || null;
  const test = tests[connectorTestKey(target)];
  const canEdit = model?.canEdit?.({ target }) ?? true;
  const canDelete = model?.canDelete?.({ target }) ?? true;

  return (
    <tr className="align-top hover:bg-stone-50">
      <ConnectorKindCell target={target} catalog={catalog} />
      <TargetCell target={target} endpoint={endpoint} />
      <td className="px-4 py-4">
        <div className="grid gap-1.5">
          <ProfilesCell target={target} />
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
            <RowActionsTemplate
              target={target}
              onOperation={onOperation}
              onUnderConstruction={onUnderConstruction}
            />
          ) : (
            <ConnectorTemplateNotFound kind={target.connector_kind} slot="row-actions" />
          )}
        </div>
      </td>
      <td className="px-4 py-4">
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" className="h-9 w-9 px-0" title="Test connection" disabled={test?.state === "testing"} onClick={() => onTestConnector(target)}>
            <PlugZap className="h-4 w-4" />
          </Button>
          <Button type="button" variant="outline" className="h-9 w-9 px-0" title="Edit connector" disabled={!canEdit} onClick={() => canEdit && onEdit(target)}>
            <Edit3 className="h-4 w-4" />
          </Button>
          <Button type="button" variant="outline" className="h-9 w-9 px-0" title="Delete connector" disabled={!canDelete} onClick={() => canDelete && onDelete(target)}>
            <Trash2 className="h-4 w-4" />
          </Button>
        </div>
      </td>
    </tr>
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
  return (
    <span className="flex items-center gap-1 text-xs text-red-800 dark-status-bad" title={value.error || value.data?.message || "Connection test failed"}>
      <CircleX className="h-3.5 w-3.5 shrink-0" />
      Failed
    </span>
  );
}

function AddConnectorMenu({ catalog, onAdd }) {
  return (
    <div className="absolute right-0 top-12 z-40 w-[360px] overflow-hidden rounded-lg border border-stone-200 bg-white p-2 shadow-xl dark-panel">
      <div className="grid gap-1">
        {supportedConnectorKinds.map((kind) => {
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
      </div>
    </div>
  );
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
            {(dialog?.details || []).filter((item) => item.value).map((item) => (
              <p className="mt-1 first:mt-0" key={item.label}>
                <span className="font-semibold">{item.label}: </span>
                <span className={item.label === "Reference" ? "font-mono text-xs" : ""}>{item.value}</span>
              </p>
            ))}
          </div>
          {dialog?.notice ? <Notice tone="warn">{dialog.notice}</Notice> : null}
          {state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null}
          <div className="grid gap-2 sm:grid-cols-2">
            {(dialog?.actions || [{ label: "Cancel", action: "close", variant: "outline" }, { label: "Delete connector", removeKey: false }]).map((action) => (
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
