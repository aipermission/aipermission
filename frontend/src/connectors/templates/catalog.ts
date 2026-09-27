import type { ComponentProps } from "react";
import type { Badge } from "../../components/ui/badge";
import type { NetworkTransportDescriptor } from "./_shared/network-transport-contract";

export type ConnectorMetadata = {
  kind: string;
  label: string;
  summary: string;
  version: string;
  icon: string;
  badge_tone?: ComponentProps<typeof Badge>["tone"];
  profile_lifecycle: "standard" | "custom";
  network_transport?: NetworkTransportDescriptor;
};

const metadataModules = import.meta.glob<ConnectorMetadata>("./*/metadata.json", { eager: true, import: "default" });

export const allowedConnectorIcons = Object.freeze(["database", "key", "mail", "server"]);

export const connectorTemplateMetadata: Readonly<Record<string, Readonly<ConnectorMetadata>>> = Object.freeze(
  Object.fromEntries(
    Object.entries(metadataModules)
      .map(([path, metadata]): [string, Readonly<ConnectorMetadata>] => [connectorKindFromPath(path), Object.freeze(metadata)])
      .sort(([left], [right]) => left.localeCompare(right)),
  ),
);

export const supportedConnectorKinds = Object.freeze(Object.keys(connectorTemplateMetadata));

export function getConnectorMetadata(kind: string): Readonly<ConnectorMetadata> | null {
  return Object.hasOwn(connectorTemplateMetadata, kind) ? connectorTemplateMetadata[kind] : null;
}

export function connectorKindLabel(kind: string): string {
  return getConnectorMetadata(kind)?.label || humanizeConnectorKind(kind);
}

export function connectorSummary(kind: string): string {
  return getConnectorMetadata(kind)?.summary || "Connector activity through the shared permission and approval pipeline.";
}

export function connectorBadgeTone(kind: string): NonNullable<ComponentProps<typeof Badge>["tone"]> {
  return getConnectorMetadata(kind)?.badge_tone || "neutral";
}

function connectorKindFromPath(path: string): string {
  const match = String(path).match(/^\.\/([^/]+)\/metadata\.json$/);
  if (!match) {
    throw new Error(`Invalid connector metadata path: ${path}`);
  }
  return match[1];
}

function humanizeConnectorKind(kind: string): string {
  return String(kind || "connector")
    .replace(/[_-]+/g, " ")
    .replace(/\b\w/g, (letter) => letter.toUpperCase());
}
