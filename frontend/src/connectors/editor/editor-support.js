export function connectorModelMissingMessage(kind) {
  return `Connector model not found for ${kind}.`;
}

export async function refreshAfterEditorMutation(onRefresh, setActionState, successMessage) {
  try {
    await onRefresh?.();
  } catch (error) {
    setActionState({
      state: "idle",
      error: `Saved successfully, but the list refresh failed: ${error.message || "unknown error"}`,
      message: successMessage,
    });
  }
}
