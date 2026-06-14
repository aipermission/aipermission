import { ChevronDown, Pencil, Plus, Trash2 } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { apiGet } from "../lib/api";
import { useGateway } from "../lib/gateway-context";
import { useAsyncAction } from "../lib/use-async-action";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Drawer } from "../components/ui/drawer";
import { Notice } from "../components/ui/notice";
import { ConnectorIcon, connectorKindLabel, connectorSummary } from "../connectors/templates/common";
import { supportedConnectorKinds } from "../connectors/templates/catalog";
import { ConnectorTemplateNotFound, getConnectorModel, getConnectorTemplate } from "../connectors/templates/registry";

function emptyCredentialState(kind, options = {}) {
  return getConnectorModel(kind)?.emptyCredentialState?.(options) || {};
}

export function CredentialsPage() {
  const { credentials, loadCredentials } = useGateway();
  const [connectorTargets, setConnectorTargets] = useState({ state: "loading", data: [], error: null });
  const defaultConnectorKind = supportedConnectorKinds[0] || "";
  const [drawer, setDrawer] = useState({ open: false, kind: defaultConnectorKind, mode: "create", row: null });
  const [addMenuOpen, setAddMenuOpen] = useState(false);
  const [credentialState, setCredentialState] = useState(() => emptyCredentialState(defaultConnectorKind));
  const { actionState: state, runAction } = useAsyncAction();

  const CredentialFormTemplate = getConnectorTemplate(drawer.kind)?.CredentialForm || null;
  const activeModel = getConnectorModel(drawer.kind);
  const rows = useMemo(
    () =>
      supportedConnectorKinds.flatMap((kind) => getConnectorModel(kind)?.credentialRows?.({
        credentials: credentials.data,
        targets: connectorTargets.data,
      }) || []),
    [credentials.data, connectorTargets.data]
  );
  const credentialFormProps = activeModel?.credentialFormProps?.({
    targets: connectorTargets.data,
    formState: credentialState,
    setFormState: setCredentialState,
    formMode: drawer.mode,
    state,
    onSubmit: saveCredential,
  }) || {};

  useEffect(() => {
    void loadConnectorTargets();
  }, []);

  async function loadConnectorTargets() {
    setConnectorTargets((current) => ({ ...current, state: "loading", error: null }));
    try {
      const data = await apiGet("/api/connector-targets");
      const items = await Promise.all((data.items || []).map((target) => apiGet(`/api/connector-targets/${target.id}`)));
      setConnectorTargets({ state: "ready", data: items, error: null });
      return items;
    } catch (error) {
      setConnectorTargets({ state: "error", data: [], error: error.message });
      return [];
    }
  }

  async function refreshCredentials() {
    await Promise.all([loadCredentials(), loadConnectorTargets()]);
  }

  function openCredentialDrawer(kind) {
    setAddMenuOpen(false);
    setDrawer({ open: true, kind, mode: "create", row: null });
    setCredentialState(emptyCredentialState(kind, { targets: connectorTargets.data }));
  }

  function openEditCredential(row) {
    setAddMenuOpen(false);
    setDrawer({ open: true, kind: row.connector_kind, mode: "edit", row });
    setCredentialState(getConnectorModel(row.connector_kind)?.credentialStateFromRow?.({ row, targets: connectorTargets.data }) || {});
  }

  function closeDrawer() {
    setDrawer({ open: false, kind: defaultConnectorKind, mode: "create", row: null });
    setCredentialState(emptyCredentialState(defaultConnectorKind, { targets: connectorTargets.data }));
  }

  async function saveCredential(event, operation) {
    event.preventDefault();
    const model = getConnectorModel(drawer.kind);
    if (!model?.saveCredential) return;
    await runAction({
      pending: operation === "import" ? "importing" : "saving",
      successMessage: (result) => result?.message || "Credential saved.",
      action: async () => {
        const result = await model.saveCredential({ operation, row: drawer.row, formState: credentialState, targets: connectorTargets.data });
        closeDrawer();
        await refreshCredentials();
        return result;
      },
    });
  }

  async function deleteCredential(row) {
    const model = getConnectorModel(row.connector_kind);
    if (!model?.deleteCredential) return;
    await runAction({
      pending: "deleting",
      successMessage: "Credential deleted.",
      action: async () => {
        await model.deleteCredential({ row });
        await refreshCredentials();
      },
    });
  }

  return (
    <section className="mx-auto grid w-full max-w-6xl gap-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h3 className="text-lg font-semibold">Credentials</h3>
          <p className="text-sm text-stone-500">Create connector credential profiles for built-in and future connectors.</p>
        </div>
        <div className="relative">
          <Button type="button" onClick={() => setAddMenuOpen((current) => !current)}>
            <Plus className="h-4 w-4" />
            Add credential
            <ChevronDown className="h-4 w-4" />
          </Button>
          {addMenuOpen ? <AddCredentialMenu onAdd={openCredentialDrawer} /> : null}
        </div>
      </div>

      {state.message ? <Notice tone="good">{state.message}</Notice> : null}
      {state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null}
      {credentials.state === "error" ? <Notice tone="bad">{credentials.error}</Notice> : null}
      {connectorTargets.state === "error" ? <Notice tone="bad">{connectorTargets.error}</Notice> : null}

      <div className="overflow-hidden rounded-lg border border-stone-200 bg-white">
        <table className="w-full table-fixed border-collapse text-left text-sm">
          <thead className="bg-stone-50 text-xs uppercase text-stone-500">
            <tr>
              <th className="w-[17%] px-4 py-3 font-semibold">Connector</th>
              <th className="w-[22%] px-4 py-3 font-semibold">Credential</th>
              <th className="w-[22%] px-4 py-3 font-semibold">Target</th>
              <th className="w-[20%] px-4 py-3 font-semibold">Public metadata</th>
              <th className="w-[12%] px-4 py-3 text-right font-semibold">Operations</th>
              <th className="w-[11%] px-4 py-3 text-right font-semibold">Actions</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-stone-200">
            {rows.map((row) => (
              <CredentialRow
                key={row.row_id}
                row={row}
                onEdit={openEditCredential}
                onDelete={deleteCredential}
                busy={state.state !== "idle" && state.state !== "error"}
              />
            ))}
          </tbody>
        </table>
        {credentials.state === "loading" || connectorTargets.state === "loading" ? (
          <div className="p-4">
            <Notice>Loading credentials...</Notice>
          </div>
        ) : null}
        {credentials.state === "ready" && connectorTargets.state === "ready" && rows.length === 0 ? (
          <div className="p-4">
            <Notice>Create your first connector credential.</Notice>
          </div>
        ) : null}
      </div>

      <Drawer
        open={drawer.open}
        title={`${drawer.mode === "edit" ? "Edit" : "Add"} ${connectorKindLabel(drawer.kind)} credential`}
        description={
          drawer.mode === "edit"
            ? "Update the connector credential profile metadata. Secrets are only replaced when you enter a new value."
            : "Choose the connector credential type, then fill the connector-specific profile form."
        }
        onClose={closeDrawer}
      >
        {CredentialFormTemplate ? (
          <CredentialFormTemplate {...credentialFormProps} />
        ) : (
          <ConnectorTemplateNotFound kind={drawer.kind} slot="credential-form" />
        )}
      </Drawer>
    </section>
  );
}

function AddCredentialMenu({ onAdd }) {
  return (
    <div className="absolute right-0 top-11 z-30 w-80 overflow-hidden rounded-lg border border-stone-200 bg-white shadow-xl">
      {supportedConnectorKinds.map((kind) => (
        <button
          className="flex w-full gap-3 border-b border-stone-100 px-4 py-3 text-left transition last:border-b-0 hover:bg-stone-50"
          key={kind}
          type="button"
          onClick={() => onAdd(kind)}
        >
          <ConnectorIcon kind={kind} className="mt-0.5 h-4 w-4 shrink-0 text-emerald-900" />
          <span className="min-w-0">
            <span className="block font-semibold">{connectorKindLabel(kind)}</span>
            <span className="mt-1 block text-xs text-stone-500">{connectorSummary(kind)}</span>
          </span>
        </button>
      ))}
    </div>
  );
}

function CredentialRow({ row, onEdit, onDelete, busy }) {
  const CredentialRowActionsTemplate = getConnectorTemplate(row.connector_kind)?.CredentialRowActions || null;
  return (
    <tr className="align-top" key={row.row_id}>
      <td className="px-4 py-4">
        <div className="flex min-w-0 items-center gap-2">
          <ConnectorIcon kind={row.connector_kind} className="h-4 w-4 shrink-0 text-emerald-900" />
          <span className="truncate font-semibold">{row.connector_label}</span>
        </div>
      </td>
      <td className="px-4 py-4">
        <div className="grid gap-1">
          <span className="truncate font-semibold">{row.name}</span>
          <Badge className="w-fit">{row.kind}</Badge>
        </div>
      </td>
      <td className="px-4 py-4">
        <div className="grid gap-1">
          <span className="truncate text-sm text-stone-700">{row.target_label}</span>
          {row.target_detail ? <span className="truncate font-mono text-xs text-stone-500">{row.target_detail}</span> : null}
        </div>
      </td>
      <td className="px-4 py-4">
        <div className="grid gap-1 text-xs text-stone-500">
          {row.metadata.map((item) => (
            <span className="truncate" key={item}>
              {item}
            </span>
          ))}
        </div>
      </td>
      <td className="px-4 py-4">
        <div className="flex justify-end gap-2">
          {CredentialRowActionsTemplate ? <CredentialRowActionsTemplate row={row} /> : null}
          {!CredentialRowActionsTemplate ? <span className="text-xs text-stone-400">None</span> : null}
        </div>
      </td>
      <td className="px-4 py-4">
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" className="h-9 w-9 px-0" title="Edit credential" onClick={() => onEdit(row)} disabled={busy}>
            <Pencil className="h-4 w-4" />
          </Button>
          <Button
            type="button"
            variant="outline"
            className="h-9 w-9 px-0"
            title={row.delete_disabled ? row.delete_disabled : "Delete credential"}
            onClick={() => onDelete(row)}
            disabled={Boolean(row.delete_disabled) || busy}
          >
            <Trash2 className="h-4 w-4" />
          </Button>
        </div>
      </td>
    </tr>
  );
}
