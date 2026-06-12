import postgresMetadata from "./postgres/metadata.json";
import sshMetadata from "./ssh/metadata.json";

export const connectorTemplateMetadata = Object.freeze({
  ssh: Object.freeze(sshMetadata),
  postgres: Object.freeze(postgresMetadata),
});

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

function humanizeConnectorKind(kind) {
  return String(kind || "connector")
    .replace(/[_-]+/g, " ")
    .replace(/\b\w/g, (letter) => letter.toUpperCase());
}
