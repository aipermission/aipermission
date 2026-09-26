export function optionalConsoleText(value: unknown, label: string, field: string): string | undefined {
  if (value === undefined || typeof value === "string") return value;
  throw invalid(label, field);
}

export function optionalConsoleNumber(value: unknown, label: string, field: string): number | undefined {
  if (value === undefined || (typeof value === "number" && Number.isFinite(value))) return value;
  throw invalid(label, field);
}

export function optionalConsolePort(value: unknown, label: string): string | number | undefined {
  return optionalConsoleTextOrNumber(value, label, "port");
}

export function optionalConsoleTextOrNumber(value: unknown, label: string, field: string): string | number | undefined {
  if (typeof value === "string") return value;
  return optionalConsoleNumber(value, label, field);
}

export function optionalConsoleBoolean(value: unknown, label: string, field: string): boolean | undefined {
  if (value === undefined || typeof value === "boolean") return value;
  throw invalid(label, field);
}

function invalid(label: string, field: string): Error {
  return new Error(`Invalid ${label} console target ${field}.`);
}
