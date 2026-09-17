export function failedResource(current, error, overrides = {}) {
  return {
    ...current,
    ...overrides,
    state: "error",
    error: error instanceof Error ? error.message : String(error),
  };
}
