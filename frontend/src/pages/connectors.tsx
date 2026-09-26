import { RefreshCcw } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { Button } from "../components/ui/button";
import { Notice } from "../components/ui/notice";
import { useConnectorInventory } from "../connectors/editor/use-connector-inventory";
import { useConnectorFamilyHost } from "../connectors/editor/use-connector-family-host";
import { ConnectorFamilyProviders } from "../connectors/editor/connector-family-providers";
import { AddConnectorMenu } from "../connectors/editor/connector-page-dialogs";
import { ConnectorTargetsTable } from "../connectors/editor/connector-targets-table";
import { connectorKindLabel } from "../connectors/templates/common";
import { connectorFamilies } from "../connectors/templates/connector-family-registry";
import { useGateway } from "../lib/gateway-context";

function resolveTableTemplate(kind: string) {
  return connectorFamilies[kind]?.tableTemplate || null;
}

export function ConnectorsPage() {
  const { targets: unifiedTargets, credentials, loadTargets: loadUnifiedTargets } = useGateway();
  const [toast, setToast] = useState("");
  const toastTimer = useRef<number | undefined>(undefined);
  const [connectorSearch, setConnectorSearch] = useState("");
  const [collapsedProjects, setCollapsedProjects] = useState<Record<string, boolean>>({});
  const inventory = useConnectorInventory({ loadUnifiedTargets });
  const { catalog, targets, projects, profileSelections, availableConnectorKinds, warnings, defaultProjectID } = inventory;
  const host = useConnectorFamilyHost();
  const firstCredentialID = useMemo(() => (credentials.data[0] ? String(credentials.data[0].id) : ""), [credentials.data]);
  const families = useMemo(() => Object.values(connectorFamilies), []);
  const connectorOptions = useMemo(
    () =>
      availableConnectorKinds.map((kind) => ({
        kind,
        label: catalog.data.find((entry) => entry.kind === kind)?.label || connectorKindLabel(kind),
      })),
    [availableConnectorKinds, catalog.data],
  );
  useEffect(() => () => window.clearTimeout(toastTimer.current), []);

  function showUnderConstruction(label: string) {
    window.clearTimeout(toastTimer.current);
    setToast(`${label} is under construction.`);
    toastTimer.current = window.setTimeout(() => setToast(""), 2200);
  }

  return (
    <ConnectorFamilyProviders
      families={families}
      targets={targets.data}
      credentials={credentials.data}
      projects={projects.data}
      firstCredentialID={firstCredentialID}
      defaultProjectID={defaultProjectID}
      connectorOptions={connectorOptions}
      busy={host.busy}
      register={host.register}
      onOpen={host.onOpen}
      onSelectKind={host.openCreate}
      onStateChange={host.onStateChange}
      onTestsChange={host.onTestsChange}
      refresh={inventory.refresh}
    >
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
            <AddConnectorMenu catalog={catalog} onAdd={host.openCreate} />
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
        {host.state.message ? <Notice tone="good">{host.state.message}</Notice> : null}
        {host.state.state === "error" ? <Notice tone="bad">{host.state.error}</Notice> : null}
        {toast ? (
          <div className="fixed right-5 top-5 z-[80] rounded-md border border-stone-700 bg-stone-950 px-4 py-3 text-sm font-semibold text-white shadow-xl">
            {toast}
          </div>
        ) : null}
        <ConnectorTargetsTable
          resolveTemplate={resolveTableTemplate}
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
          tests={host.tests}
          onSelectProfile={inventory.selectProfile}
          onTestConnector={host.test}
          onUnderConstruction={showUnderConstruction}
          onEdit={host.openEdit}
          onDelete={host.requestDelete}
        />
      </section>
    </ConnectorFamilyProviders>
  );
}
