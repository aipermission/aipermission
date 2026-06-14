const metadataModules = import.meta.glob("./*/metadata.json", { eager: true, import: "default" });

export const connectorTemplateMetadata = Object.freeze(
  Object.fromEntries(
    Object.entries(metadataModules)
      .map(([path, metadata]) => [connectorKindFromPath(path), Object.freeze(metadata)])
      .sort(([left], [right]) => left.localeCompare(right))
  )
);

export const supportedConnectorKinds = Object.freeze(Object.keys(connectorTemplateMetadata));

export function getConnectorMetadata(kind) {
  return connectorTemplateMetadata[kind] || null;
}

export function connectorKindLabel(kind) {
  return getConnectorMetadata(kind)?.label || humanizeConnectorKind(kind);
}

export function connectorSummary(kind) {
  return getConnectorMetadata(kind)?.summary || "Connector activity through the shared permission and approval pipeline.";
}

export function connectorBadgeTone(kind) {
  return getConnectorMetadata(kind)?.badge_tone || "neutral";
}

function connectorKindFromPath(path) {
  const match = String(path).match(/^\.\/([^/]+)\/metadata\.json$/);
  if (!match) {
    throw new Error(`Invalid connector metadata path: ${path}`);
  }
  return match[1];
}

function humanizeConnectorKind(kind) {
  return String(kind || "connector")
    .replace(/[_-]+/g, " ")
    .replace(/\b\w/g, (letter) => letter.toUpperCase());
}
