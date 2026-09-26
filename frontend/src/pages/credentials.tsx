import { useMemo } from "react";
import { useGateway } from "../lib/gateway-context";
import { Notice } from "../components/ui/notice";
import { supportedConnectorKinds } from "../connectors/templates/catalog";
import { credentialFamilies } from "../connectors/templates/credential-registry";
import { useCredentialInventory } from "../connectors/editor/use-credential-inventory";
import { useCredentialFamilyHost } from "../connectors/editor/use-credential-family-host";
import { CredentialFamilyBoundary } from "../connectors/editor/credential-family-boundary";
import { AddCredentialMenu } from "../connectors/editor/credential-menu";

export { AddCredentialMenu } from "../connectors/editor/credential-menu";

export function CredentialsPage() {
  const { credentials, loadCredentials } = useGateway();
  const { catalog: connectorCatalog, targets: connectorTargets, refresh: refreshInventory } = useCredentialInventory();
  const availableKinds = useMemo(() => {
    const backendKinds = new Set(connectorCatalog.data.map((item) => item.kind));
    return supportedConnectorKinds.filter((kind) => backendKinds.has(kind));
  }, [connectorCatalog.data]);
  const host = useCredentialFamilyHost();
  const rowsReady = availableKinds.every((kind) => host.rowCounts.has(kind));
  const rowCount = availableKinds.reduce((count, kind) => count + (host.rowCounts.get(kind) || 0), 0);
  async function refreshCredentials() {
    await Promise.all([loadCredentials(), refreshInventory()]);
  }

  return (
    <section className="mx-auto grid w-full max-w-6xl gap-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-lg font-semibold">Credentials</h1>
          <p className="text-sm text-stone-500">Create connector credential profiles for built-in and future connectors.</p>
        </div>
        <AddCredentialMenu kinds={availableKinds} onAdd={host.openCreate} />
      </div>
      {host.state.message ? <Notice tone="good">{host.state.message}</Notice> : null}
      {host.state.state === "error" ? <Notice tone="bad">{host.state.error}</Notice> : null}
      {connectorCatalog.state === "error" ? <Notice tone="bad">{connectorCatalog.error}</Notice> : null}
      {credentials.state === "error" ? <Notice tone="bad">{credentials.error}</Notice> : null}
      {(credentials.errors || []).map((error) => (
        <Notice tone="warn" key={error}>
          Credential resource load warning: {error}
        </Notice>
      ))}
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
            {availableKinds.map((kind) => {
              const family = credentialFamilies[kind];
              return (
                <CredentialFamilyBoundary key={kind} kind={kind} targets={connectorTargets.data} credentials={credentials.data}>
                  <family.Rows
                    targets={connectorTargets.data}
                    credentials={credentials.data}
                    busy={host.busy}
                    register={host.register}
                    onOpen={host.onOpen}
                    onStateChange={host.onStateChange}
                    onRowsChange={host.onRowsChange}
                    refresh={refreshCredentials}
                  />
                </CredentialFamilyBoundary>
              );
            })}
          </tbody>
        </table>
        {credentials.state === "loading" || connectorCatalog.state === "loading" || connectorTargets.state === "loading" ? (
          <div className="p-4">
            <Notice>Loading credentials...</Notice>
          </div>
        ) : null}
        {credentials.state === "ready" &&
        connectorCatalog.state === "ready" &&
        connectorTargets.state === "ready" &&
        rowsReady &&
        rowCount === 0 ? (
          <div className="p-4">
            <Notice>Create your first connector credential.</Notice>
          </div>
        ) : null}
      </div>
    </section>
  );
}
