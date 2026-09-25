import type { Dispatch, SetStateAction } from "react";
import { errorMessage } from "../../lib/errors";
import type { AsyncActionState } from "../../lib/use-async-action";

export function connectorModelMissingMessage(kind: string): string {
  return `Connector model not found for ${kind}.`;
}

export async function refreshAfterEditorMutation(
  onRefresh: (() => Promise<void> | void) | undefined,
  setActionState: Dispatch<SetStateAction<AsyncActionState>>,
  successMessage: string,
): Promise<void> {
  try {
    await onRefresh?.();
  } catch (error) {
    setActionState({
      state: "idle",
      error: `Saved successfully, but the list refresh failed: ${errorMessage(error, "unknown error")}`,
      message: successMessage,
    });
  }
}
