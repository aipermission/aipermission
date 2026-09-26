export type ConnectorConnectionTest = {
  ok: boolean;
  message?: string;
  stdout?: string;
  stderr?: string;
  [field: string]: unknown;
};

export function connectorConnectionTestResponse(value: unknown): ConnectorConnectionTest {
  const data = record(value);
  if (typeof data.ok !== "boolean") throw invalid();
  for (const key of ["message", "stdout", "stderr"]) if (data[key] !== undefined && typeof data[key] !== "string") throw invalid();
  return data as ConnectorConnectionTest;
}

export type SavedConnectorTarget = { id: number; profiles?: { id: number }[]; [field: string]: unknown };
export function savedConnectorTargetResponse(value: unknown): SavedConnectorTarget {
  const data = record(value);
  if (!positiveID(data.id)) throw invalid();
  if (data.profiles !== undefined && (!Array.isArray(data.profiles) || !data.profiles.every((profile) => positiveID(record(profile).id))))
    throw invalid();
  return data as SavedConnectorTarget;
}

function positiveID(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value > 0;
}
function record(value: unknown): Record<string, unknown> {
  if (value === null || typeof value !== "object" || Array.isArray(value)) throw invalid();
  return value as Record<string, unknown>;
}
function invalid() {
  return new Error("Invalid connector management response from gateway.");
}
