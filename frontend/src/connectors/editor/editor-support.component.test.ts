import { expect, it, vi } from "vitest";
import { connectorModelMissingMessage, refreshAfterEditorMutation } from "./editor-support";

it("keeps a successful save visible when the follow-up list refresh fails", async () => {
  const setActionState = vi.fn();
  await refreshAfterEditorMutation(
    async () => {
      throw new Error("offline");
    },
    setActionState,
    "Credential saved.",
  );
  expect(setActionState).toHaveBeenCalledWith({
    state: "idle",
    error: "Saved successfully, but the list refresh failed: offline",
    message: "Credential saved.",
  });
  expect(connectorModelMissingMessage("example")).toBe("Connector model not found for example.");
});
