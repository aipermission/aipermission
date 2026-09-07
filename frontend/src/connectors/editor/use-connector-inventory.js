import { useEffect, useEffectEvent, useMemo, useState } from "react";
import { apiGet } from "../../lib/api";
import { useRequestGuard } from "../../lib/request-guard";
import { supportedConnectorKinds } from "../templates/catalog";

const loadingCatalog = { state: "loading", data: [], details: {}, detailFailures: [], error: null };
const loadingCollection = { state: "loading", data: [], error: null };

export function targetProfileSelectionKey(target) {
  return `${target?.connector_kind || ""}:${target?.id || ""}`;
}

export function useConnectorInventory({ loadUnifiedTargets }) {
  const [catalog, setCatalog] = useState(loadingCatalog);
  const [targets, setTargets] = useState(loadingCollection);
  const [projects, setProjects] = useState(loadingCollection);
  const [profileSelections, setProfileSelections] = useState({});
  const guard = useRequestGuard("connector-inventory");
  const initialize = useEffectEvent(() => {
    void loadCatalog();
    void refresh();
  });
  const reconcileProfileSelections = useEffectEvent(() => {
    setProfileSelections((current) => {
      const next = {};
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
    const request = guard.begin("projects");
    setProjects((current) => ({ ...current, state: "loading", error: null }));
    try {
      const data = await apiGet("/api/projects", { signal: request.signal });
      if (request.isCurrent()) setProjects({ state: "ready", data: data.items || [], error: null });
    } catch (error) {
      if (request.isCurrent()) setProjects({ state: "error", data: [], error: error.message });
    } finally {
      request.complete();
    }
  }

  async function loadCatalog() {
    const request = guard.begin("catalog");
    setCatalog((current) => ({ ...current, state: "loading", error: null }));
    try {
      const data = await apiGet("/api/connectors", { signal: request.signal });
      const details = {};
      const detailFailures = [];
      await Promise.all(
        (data.items || []).map(async (item) => {
          try {
            details[item.kind] = await apiGet(`/api/connectors/${item.kind}`, { signal: request.signal });
          } catch (error) {
            if (!request.signal.aborted)
              detailFailures.push({ kind: item.kind, error: error.message || "failed to load connector details" });
          }
        }),
      );
      if (request.isCurrent()) setCatalog({ state: "ready", data: data.items || [], details, detailFailures, error: null });
    } catch (error) {
      if (request.isCurrent()) setCatalog({ state: "error", data: [], details: {}, detailFailures: [], error: error.message });
    } finally {
      request.complete();
    }
  }

  async function loadTargets() {
    const request = guard.begin("targets");
    setTargets((current) => ({ ...current, state: "loading", error: null }));
    try {
      const data = await apiGet("/api/connector-targets/inventory", { signal: request.signal });
      if (request.isCurrent()) setTargets({ state: "ready", data: data.items || [], error: null });
    } catch (error) {
      if (request.isCurrent()) setTargets({ state: "error", data: [], error: error.message });
    } finally {
      request.complete();
    }
  }

  function selectProfile(target, profileID) {
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

function connectorCatalogWarnings(catalog) {
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
