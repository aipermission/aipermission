export function connectorActionBusy(state) {
  if (!state) return true;
  return state.state !== "idle" && state.state !== "error";
}
