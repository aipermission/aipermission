export function formatDockerLogs(logs) {
  return String(logs || "")
    .split("\n")
    .map((line) => formatDockerLogLine(line))
    .join("\n");
}

export function formatDockerLogLine(line) {
  const text = String(line || "");
  if (!text.trim()) return text;
  const firstBrace = text.indexOf("{");
  const lastBrace = text.lastIndexOf("}");
  if (firstBrace < 0 || lastBrace <= firstBrace) return text;
  const prefixText = text.slice(0, firstBrace).trim();
  const jsonText = text.slice(firstBrace, lastBrace + 1);
  try {
    const payload = JSON.parse(jsonText);
    const timestamp = payload.Timestamp || prefixText || "";
    const level = payload.Level || payload.level || payload.Severity || "";
    const message = payload.MessageTemplate || payload.RenderedMessage || payload.Message || payload.message || jsonText;
    const lines = [];
    const prefix = [timestamp, level ? `[${level}]` : ""].filter(Boolean).join(" ");
    lines.push(prefix || "Docker log");
    lines.push(`  ${String(message)}`);
    if (payload.Exception) lines.push(`  Exception: ${String(payload.Exception)}`);
    const properties = payload.Properties && typeof payload.Properties === "object" ? payload.Properties : null;
    if (properties) {
      const details = Object.entries(properties)
        .slice(0, 8)
        .map(([key, value]) => `${key}=${shortValue(value)}`);
      if (details.length > 0) lines.push(`  ${details.join(" ")}`);
    }
    return lines.join("\n");
  } catch {
    return text;
  }
}

export function stripSlash(value) {
  return String(value || "").replace(/^\//, "");
}

export function arrayOrString(value) {
  if (Array.isArray(value)) return value.join(" ");
  return value;
}

export function summarizePorts(ports) {
  if (!ports || typeof ports !== "object") return "";
  return Object.entries(ports)
    .map(([containerPort, bindings]) => {
      if (!Array.isArray(bindings) || bindings.length === 0) return containerPort;
      return bindings.map((binding) => `${binding.HostIp || "0.0.0.0"}:${binding.HostPort || ""}->${containerPort}`).join("\n");
    })
    .filter(Boolean)
    .join("\n");
}

export function summarizeNetworks(networks) {
  if (!networks || typeof networks !== "object") return "";
  return Object.entries(networks)
    .map(([name, network]) => `${name}${network?.IPAddress ? ` ${network.IPAddress}` : ""}`)
    .join("\n");
}

export function shortValue(value) {
  const text = typeof value === "string" ? value : JSON.stringify(value);
  if (!text) return "";
  return text.length > 80 ? `${text.slice(0, 77)}...` : text;
}

export function resourceLabel(kind) {
  if (kind === "images") return "Images";
  if (kind === "networks") return "Networks";
  if (kind === "volumes") return "Volumes";
  return "Containers";
}

export function resourceTabLabel(kind) {
  if (kind === "containers") return "Ctrs";
  if (kind === "networks") return "Nets";
  if (kind === "volumes") return "Vols";
  return "Images";
}

export function resourceSingular(kind) {
  if (kind === "images") return "image";
  if (kind === "networks") return "network";
  if (kind === "volumes") return "volume";
  return "container";
}

export function resourceKey(kind, item = {}) {
  if (kind === "images") return item.id || `${item.repository || ""}:${item.tag || ""}`;
  if (kind === "networks") return item.id || item.name;
  if (kind === "volumes") return item.name;
  return item.id || item.name;
}

export function resourcePrimary(kind, item = {}) {
  if (kind === "images") return imageRef(item);
  return item.name || item.id || "-";
}

export function resourceSecondary(kind, item = {}) {
  if (kind === "containers") return [item.image, item.compose_project ? `project ${item.compose_project}` : ""].filter(Boolean).join(" · ");
  if (kind === "images") return [item.id, item.size].filter(Boolean).join(" · ");
  if (kind === "networks") return [item.driver, item.scope].filter(Boolean).join(" · ");
  if (kind === "volumes") return [item.driver, item.mountpoint].filter(Boolean).join(" · ");
  return "";
}

export function resourceTertiary(kind, item = {}) {
  if (kind === "containers")
    return [item.status, item.compose_service ? `service ${item.compose_service}` : ""].filter(Boolean).join(" · ");
  if (kind === "images")
    return [item.created_since || item.created_at, item.containers ? `${item.containers} containers` : ""].filter(Boolean).join(" · ");
  if (kind === "networks") return item.containers ? `${item.containers} visible containers` : item.labels || "";
  if (kind === "volumes") return item.containers ? `${item.containers} visible containers` : item.labels || "";
  return "";
}

export function resourceStatus(kind, item = {}) {
  if (kind === "containers") return item.health || item.state || "unknown";
  if (kind === "images") return item.size || "image";
  if (kind === "networks") return item.driver || "network";
  if (kind === "volumes") return item.driver || "volume";
  return "item";
}

export function resourceTone(kind, item = {}) {
  if (kind === "containers") {
    if (item.health === "unhealthy") return "bad";
    if (item.state === "running") return "good";
    return "neutral";
  }
  return "neutral";
}

export function resourceSearchValues(kind, item = {}) {
  if (kind === "images") return [imageRef(item), item.id, item.size, item.created_since, item.created_at];
  if (kind === "networks") return [item.name, item.id, item.driver, item.scope, item.labels];
  if (kind === "volumes") return [item.name, item.driver, item.scope, item.mountpoint, item.labels];
  return [item.name, item.id, item.image, item.state, item.status, item.health, item.ports, item.compose_project, item.compose_service];
}

export function resourcePlaceholder(kind) {
  if (kind === "images") return "Select an image to inspect read-only image metadata.";
  if (kind === "networks") return "Select a network to inspect read-only network metadata.";
  if (kind === "volumes") return "Select a volume to inspect read-only volume metadata.";
  return "Choose a visible container to read logs, inspect metadata, or run lifecycle actions.";
}

export function imageRef(item = {}) {
  if (!item.repository) return item.id || "-";
  if (!item.tag || item.tag === "<none>") return item.repository;
  return `${item.repository}:${item.tag}`;
}
