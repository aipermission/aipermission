export const resourceTabs = Object.freeze([
  { key: "workloads", label: "Workloads", action: "list_workloads", output: "workloads" },
  { key: "pods", label: "Pods", action: "list_pods", output: "pods" },
  { key: "services", label: "Services", action: "list_services", output: "services" },
  { key: "ingress", label: "Ingress", action: "list_ingress", output: "ingress" },
  { key: "nodes", label: "Nodes", action: "list_nodes", output: "nodes" },
  { key: "events", label: "Events", action: "list_events", output: "events" },
]);

export function resourceKey(tab, item) {
  if (!item) return "";
  if (tab === "nodes") return item.name || "";
  return `${item.namespace || ""}/${item.kind || tab}/${item.name || item.reason || item.message || ""}`;
}

export function resourceTitle(tab, item) {
  if (!item) return "";
  if (tab === "events") return `${item.type || "Event"} ${item.reason || ""}`.trim();
  if (tab === "workloads") return `${item.namespace}/${item.kind}/${item.name}`;
  if (tab === "pods") return item.node || item.name;
  if (tab === "nodes") return item.name;
  return `${item.namespace}/${item.name}`;
}

export function resourceSubtitle(tab, item) {
  if (!item) return "";
  if (tab === "workloads") return `ready ${item.ready || "-"} · image ${item.image || "-"}`;
  if (tab === "pods") return `${item.namespace}/${item.name}`;
  if (tab === "services") return `${item.type || "-"} · ${item.cluster_ip || "-"} · ${item.ports || "no ports"}`;
  if (tab === "ingress") return `${item.hosts || "no hosts"} · ${item.class || "no class"}`;
  if (tab === "nodes") return `ready ${item.ready || "-"} · ${item.roles || "-"} · ${item.version || "-"}`;
  return `${item.namespace || "-"} · ${item.object || "-"} · ${item.message || ""}`;
}

export function resourceTertiary(tab, item) {
  if (!item) return "";
  if (tab === "pods") return `ready ${item.ready || "-"} · restarts ${item.restarts || 0} · ${item.phase || "-"}`;
  if (tab === "workloads") return `${item.namespace || "-"} · age ${item.age || "-"}`;
  if (tab === "services") return `age ${item.age || "-"} · external ${item.external_ip || "-"}`;
  if (tab === "ingress") return `namespace ${item.namespace || "-"} · age ${item.age || "-"}`;
  if (tab === "nodes") return `age ${item.age || "-"} · status ${item.ready || "-"}`;
  return item.message || "";
}

export function resourceStatus(tab, item) {
  if (!item) return "";
  if (tab === "pods") return item.phase || "";
  if (tab === "workloads") return item.ready || "";
  if (tab === "services") return item.type || "";
  if (tab === "nodes") return item.ready || "";
  if (tab === "events") return item.type || "";
  return "";
}

export function resourceTone(tab, item) {
  const value = String(resourceStatus(tab, item)).toLowerCase();
  if (tab === "events" && value === "warning") return "warn";
  if (value.includes("running") || value.includes("ready") || value.match(/^\d+\/\d+$/)) return "good";
  if (value.includes("pending") || value.includes("unknown")) return "warn";
  if (value.includes("failed") || value.includes("error") || value.includes("crash")) return "bad";
  return "neutral";
}

export function resourceSearchValues(tab, item) {
  return [
    resourceTitle(tab, item),
    resourceSubtitle(tab, item),
    resourceTertiary(tab, item),
    item.namespace,
    item.name,
    item.kind,
    item.reason,
    item.message,
    item.hosts,
    item.image,
    item.node,
  ];
}

export function resourceTypeForWorkload(resource) {
  const kind = String(resource?.kind || "").toLowerCase();
  if (kind === "statefulset") return "statefulset";
  if (kind === "daemonset") return "daemonset";
  return "deployment";
}
