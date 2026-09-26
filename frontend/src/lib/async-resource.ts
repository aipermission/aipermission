export function failedResource<Resource extends object>(current: Resource, error: unknown, overrides: Partial<Resource> = {}) {
  return {
    ...current,
    ...overrides,
    state: "error",
    error: error instanceof Error ? error.message : String(error),
  };
}

export function pollReadOptions(signal: AbortSignal | undefined, generation?: number): { signal: AbortSignal | undefined; timeoutMs?: number } {
  return generation === undefined ? { signal } : { signal, timeoutMs: 4000 };
}
