import { useEffect, useEffectEvent, useMemo, useState } from "react";
import { apiGet } from "../../lib/api";
import { loadProjectOptions } from "../../lib/load-project-options";
import { useRequestGuard } from "../../lib/request-guard";
import { supportedConnectorKinds } from "../templates/catalog";

import { errorMessage } from "../../lib/errors";
import type { ProjectOptionsState } from "../../lib/load-project-options";
import { connectorInventoryResponse, type InventoryTarget } from "../../lib/gateway-contracts/connector-inventory-contract";
import {
  connectorCatalogResponse,
  connectorCatalogDetailResponse,
  type ConnectorCatalogItem,
  type ConnectorCatalogDetail,
} from "../../lib/gateway-contracts/connector-catalog-contract";
export type ConnectorCatalogState = {
  state: string;
  data: ConnectorCatalogItem[];
  details: Record<string, ConnectorCatalogDetail>;
  detailFailures: { kind: string; error: string }[];
  error: string | null;
};
export type ConnectorInventoryState = { state: string; data: InventoryTarget[]; error: string | null };
const loadingCatalog: ConnectorCatalogState = { state: "loading", data: [], details: {}, detailFailures: [], error: null };
const loadingCollection = { state: "loading", data: [], error: null };

export function targetProfileSelectionKey(target: Pick<InventoryTarget, "id" | "connector_kind"> | null | undefined) {
  return `${target?.connector_kind || ""}:${target?.id || ""}`;
}

export function useConnectorInventory({ loadUnifiedTargets }: { loadUnifiedTargets: () => unknown | Promise<unknown> }) {
  const [catalog, setCatalog] = useState(loadingCatalog);
  const [targets, setTargets] = useState<ConnectorInventoryState>(loadingCollection);
  const [projects, setProjects] = useState<ProjectOptionsState>(loadingCollection);
  const [profileSelections, setProfileSelections] = useState<Record<string, string>>({});
  const guard = useRequestGuard("connector-inventory");
  const initialize = useEffectEvent(() => {
    void loadCatalog();
    void refresh();
  });
  const reconcileProfileSelections = useEffectEvent(() => {
    setProfileSelections((current) => {
      const next: Record<string, string> = {};
      for (const target of targets.data) {
        const key = targetProfileSelectionKey(target);
        const profiles = target.profiles || [];
        if (profiles.length === 0) continue;
        const currentID = current[key];
        next[key] = profiles.some((profile) => String(profile.id) === String(currentID)) ? String(currentID) : String(profiles[0].id);
      }
      return next;
    });
  });
  const targetProfileSignature = targets.data
    .map((target) => `${target.connector_kind}:${target.id}:${(target.profiles || []).map((profile) => profile.id).join(",")}`)
    .join("|");
  const availableConnectorKinds = useMemo(() => {
    if (catalog.state !== "ready") return [];
    const backendKinds = new Set(catalog.data.map((item) => item.kind));
    return supportedConnectorKinds.filter((kind) => backendKinds.has(kind) && catalog.details[kind]);
  }, [catalog]);
  const warnings = useMemo(() => connectorCatalogWarnings(catalog), [catalog]);
  const defaultProjectID = useMemo(
    () => projects.data.find((project) => project.slug === "ungrouped")?.id || projects.data[0]?.id || "",
    [projects.data],
  );

  useEffect(() => {
    initialize();
  }, []);

  useEffect(() => {
    reconcileProfileSelections();
  }, [targetProfileSignature]);

  async function refresh() {
    await Promise.all([loadTargets(), loadProjects(), loadUnifiedTargets()]);
  }

  async function loadProjects() {
    await loadProjectOptions(guard, setProjects);
  }

  async function loadCatalog() {
    const request = guard.begin("catalog");
    setCatalog((current) => ({ ...current, state: "loading", error: null }));
    try {
      const data = await apiGet("/api/connectors", { signal: request.signal });
      if (!request.isCurrent()) return;
      const items = connectorCatalogResponse(data);
      const details: Record<string, ConnectorCatalogDetail> = {};
      const detailFailures: ConnectorCatalogState["detailFailures"] = [];
      await Promise.all(
        items.map(async (item) => {
          try {
            const detail = await apiGet(`/api/connectors/${item.kind}`, { signal: request.signal });
            if (request.isCurrent()) details[item.kind] = connectorCatalogDetailResponse(detail, item.kind);
          } catch (error) {
            if (!request.signal.aborted)
              detailFailures.push({ kind: item.kind, error: errorMessage(error, "failed to load connector details") });
          }
        }),
      );
      if (request.isCurrent()) setCatalog({ state: "ready", data: items, details, detailFailures, error: null });
    } catch (error) {
      if (request.isCurrent()) setCatalog({ state: "error", data: [], details: {}, detailFailures: [], error: errorMessage(error) });
    } finally {
      request.complete();
    }
  }

  async function loadTargets() {
    const request = guard.begin("targets");
    setTargets((current) => ({ ...current, state: "loading", error: null }));
    try {
      const data = await apiGet("/api/connector-targets/inventory", { signal: request.signal });
      if (request.isCurrent()) setTargets({ state: "ready", data: connectorInventoryResponse(data), error: null });
    } catch (error) {
      if (request.isCurrent()) setTargets({ state: "error", data: [], error: errorMessage(error) });
    } finally {
      request.complete();
    }
  }

  function selectProfile(target: InventoryTarget, profileID: number | string) {
    setProfileSelections((current) => ({ ...current, [targetProfileSelectionKey(target)]: String(profileID || "") }));
  }

  return {
    catalog,
    targets,
    projects,
    profileSelections,
    availableConnectorKinds,
    warnings,
    defaultProjectID,
    refresh,
    selectProfile,
  };
}

function connectorCatalogWarnings(catalog: ConnectorCatalogState) {
  if (catalog.state !== "ready") return [];
  const backendKinds = new Set(catalog.data.map((item) => item.kind));
  const frontendKinds = new Set(supportedConnectorKinds);
  const backendOnly = [...backendKinds].filter((kind) => !frontendKinds.has(kind)).sort();
  const frontendOnly = [...frontendKinds].filter((kind) => !backendKinds.has(kind)).sort();
  const warnings = [];
  if (backendOnly.length > 0) warnings.push(`Backend connector catalog has no matching frontend template: ${backendOnly.join(", ")}.`);
  if (frontendOnly.length > 0) warnings.push(`Frontend connector template has no matching backend connector: ${frontendOnly.join(", ")}.`);
  for (const failure of catalog.detailFailures || []) {
    warnings.push(`Backend connector detail failed for ${failure.kind}: ${failure.error}.`);
  }
  return warnings;
}
