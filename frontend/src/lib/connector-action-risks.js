export const connectorActionRiskOrder = ["read", "write", "destructive", "credential_sensitive", "other"];

const riskLabels = {
  read: "read",
  write: "write",
  destructive: "destructive",
  credential_sensitive: "credential sensitive",
  other: "other",
};

const riskGroupLabels = {
  read: "Read operations",
  write: "Write operations",
  destructive: "Destructive operations",
  credential_sensitive: "Credential-sensitive operations",
  other: "Other operations",
};

const riskDescriptions = {
  read: "read-only",
  write: "write-capable",
  destructive: "destructive",
  credential_sensitive: "credential-sensitive",
  other: "uncategorized",
};

export function normalizeConnectorActionRisk(risk) {
  const value = String(risk || "").trim();
  return connectorActionRiskOrder.includes(value) && value !== "other" ? value : "other";
}

export function connectorActionRiskLabel(risk) {
  return riskLabels[normalizeConnectorActionRisk(risk)];
}

export function connectorActionRiskTone(risk) {
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

export function connectorActionRiskGroupLabel(risk) {
  return riskGroupLabels[normalizeConnectorActionRisk(risk)];
}

export function connectorActionRiskDescription(risk, count) {
  const normalized = normalizeConnectorActionRisk(risk);
  const descriptor = riskDescriptions[normalized];
  return count > 0 ? `${count} ${descriptor} action${count === 1 ? "" : "s"}` : `No ${descriptor} actions exposed.`;
}
