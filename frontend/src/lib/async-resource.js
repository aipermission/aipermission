export function failedResource(current, error, overrides = {}) {
  return {
    ...current,
    ...overrides,
    state: "error",
    error: error instanceof Error ? error.message : String(error),
  };
}

export function pollReadOptions(signal, generation) {
  return generation === undefined ? { signal } : { signal, timeoutMs: 4000 };
}
