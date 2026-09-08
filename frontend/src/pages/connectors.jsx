import { RefreshCcw } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { Button } from "../components/ui/button";
import { Notice } from "../components/ui/notice";
import { useConnectorConnectionTests } from "../connectors/editor/use-connector-connection-tests";
import { useConnectorEditor } from "../connectors/editor/use-connector-editor";
import { useConnectorInventory } from "../connectors/editor/use-connector-inventory";
import { AddConnectorMenu, ConnectorEditorDrawer, DeleteConnectorDialog } from "../connectors/editor/connector-page-dialogs";
import { ConnectorTargetsTable } from "../connectors/editor/connector-targets-table";
import { supportedConnectorKinds } from "../connectors/templates/catalog";
import { connectorKindLabel } from "../connectors/templates/common";
import { getConnectorModel, getConnectorTemplate } from "../connectors/templates/registry";
import { useGateway } from "../lib/gateway-context";

function emptyConnectorForm(kind, options = {}) {
  return getConnectorModel(kind)?.emptyForm?.(options) || { connector_kind: kind };
}

export function ConnectorsPage() {
  const { targets: unifiedTargets, credentials, loadTargets: loadUnifiedTargets } = useGateway();
  const [connectorOperation, setConnectorOperation] = useState({ open: false, connector_kind: "", type: "", state: "idle", error: null });
  const [toast, setToast] = useState("");
  const toastTimer = useRef(null);
  const [connectorSearch, setConnectorSearch] = useState("");
  const [collapsedProjects, setCollapsedProjects] = useState({});
  const inventory = useConnectorInventory({ loadUnifiedTargets });
  const { catalog, targets, projects, profileSelections, availableConnectorKinds, warnings, defaultProjectID } = inventory;
  const firstCredentialID = useMemo(() => (credentials.data[0] ? String(credentials.data[0].id) : ""), [credentials.data]);
  const editor = useConnectorEditor({
    defaultKind: supportedConnectorKinds[0] || "",
    firstCredentialID,
    defaultProjectID,
    emptyFormForKind: emptyConnectorForm,
    modelForKind: getConnectorModel,
    onRefresh: inventory.refresh,
    onOperation: openConnectorOperation,
  });
  const connectionTests = useConnectorConnectionTests({ modelForKind: getConnectorModel, onOperation: openConnectorOperation });
  const { drawer, deleteDialog, form, actionState } = editor;
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

  useEffect(
    () => () => {
      window.clearTimeout(toastTimer.current);
    },
    [],
  );

  function showUnderConstruction(label) {
    window.clearTimeout(toastTimer.current);
    setToast(`${label} is under construction.`);
    toastTimer.current = window.setTimeout(() => setToast(""), 2200);
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
    await inventory.refresh();
  }

  return (
    <section className="mx-auto grid w-full max-w-7xl gap-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-lg font-semibold">Connectors</h1>
          <p className="text-sm text-stone-500">
            Create connector targets, attach credential profiles, then grant token permissions per action.
          </p>
        </div>
        <div className="flex gap-2">
          <Button type="button" variant="outline" onClick={inventory.refresh} disabled={targets.state === "loading"}>
            <RefreshCcw className="h-4 w-4" />
            Refresh
          </Button>
          <AddConnectorMenu catalog={catalog} onAdd={editor.openCreate} />
        </div>
      </div>

      {catalog.state === "error" ? <Notice tone="bad">{catalog.error}</Notice> : null}
      {warnings.map((warning) => (
        <Notice tone="warn" key={warning}>
          {warning}
        </Notice>
      ))}
      {targets.state === "error" ? <Notice tone="bad">{targets.error}</Notice> : null}
      {projects.state === "error" ? <Notice tone="bad">{projects.error}</Notice> : null}
      {actionState.message ? <Notice tone="good">{actionState.message}</Notice> : null}
      {actionState.state === "error" ? <Notice tone="bad">{actionState.error}</Notice> : null}
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
        tests={connectionTests.tests}
        onSelectProfile={inventory.selectProfile}
        onTestConnector={connectionTests.run}
        onOperation={setConnectorOperation}
        onUnderConstruction={showUnderConstruction}
        onEdit={editor.openEdit}
        onDelete={editor.requestDelete}
      />

      <ConnectorEditorDrawer
        drawer={drawer}
        form={form}
        state={actionState}
        connectorOptions={connectorOptions}
        projects={projects.data}
        credentials={credentials.data}
        targets={targets.data}
        activeConnectorModel={activeConnectorModel}
        activeCredential={activeCredential}
        FormTemplate={ActiveConnectorFormTemplate}
        editor={editor}
      />

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
      <DeleteConnectorDialog value={deleteDialog} state={actionState} onDelete={editor.remove} onClose={editor.closeDelete} />
    </section>
  );
}
