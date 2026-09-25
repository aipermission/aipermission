export function connectorActionBusy(state: { state: string } | null | undefined): boolean {
  if (!state) return true;
  return state.state !== "idle" && state.state !== "error";
}
