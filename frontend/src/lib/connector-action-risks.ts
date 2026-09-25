export const connectorActionRiskOrder = ["read", "write", "destructive", "credential_sensitive", "other"] as const;
export type ConnectorActionRisk = (typeof connectorActionRiskOrder)[number];

const riskLabels: Record<ConnectorActionRisk, string> = {
  read: "read",
  write: "write",
  destructive: "destructive",
  credential_sensitive: "credential sensitive",
  other: "other",
};

const riskGroupLabels: Record<ConnectorActionRisk, string> = {
  read: "Read operations",
  write: "Write operations",
  destructive: "Destructive operations",
  credential_sensitive: "Credential-sensitive operations",
  other: "Other operations",
};

const riskDescriptions: Record<ConnectorActionRisk, string> = {
  read: "read-only",
  write: "write-capable",
  destructive: "destructive",
  credential_sensitive: "credential-sensitive",
  other: "uncategorized",
};

export function normalizeConnectorActionRisk(risk: unknown): ConnectorActionRisk {
  const value = String(risk || "").trim();
  return connectorActionRiskOrder.find((candidate) => candidate === value) || "other";
}

export function connectorActionRiskLabel(risk: unknown): string {
  return riskLabels[normalizeConnectorActionRisk(risk)];
}

export function connectorActionRiskTone(risk: unknown): "good" | "bad" | "warn" | "neutral" {
  switch (normalizeConnectorActionRisk(risk)) {
    case "read":
      return "good";
    case "destructive":
    case "credential_sensitive":
      return "bad";
    case "write":
      return "warn";
    default:
      return "neutral";
  }
}

export function connectorActionRiskGroupLabel(risk: unknown): string {
  return riskGroupLabels[normalizeConnectorActionRisk(risk)];
}

export function connectorActionRiskDescription(risk: unknown, count: number): string {
  const normalized = normalizeConnectorActionRisk(risk);
  const descriptor = riskDescriptions[normalized];
  return count > 0 ? `${count} ${descriptor} action${count === 1 ? "" : "s"}` : `No ${descriptor} actions exposed.`;
}
