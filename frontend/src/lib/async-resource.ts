export function failedResource<Resource extends object>(current: Resource, error: unknown, overrides: Partial<Resource> = {}) {
  return {
    ...current,
    ...overrides,
    state: "error",
    error: error instanceof Error ? error.message : String(error),
  };
}

export function pollReadOptions(signal: AbortSignal, generation?: number): { signal: AbortSignal; timeoutMs?: number } {
  return generation === undefined ? { signal } : { signal, timeoutMs: 4000 };
}
