import { expect, it } from "vitest";
import {
  connectorActionCode,
  connectorActionError,
  connectorActionPending,
  connectorActionRequestID,
  connectorActionResultResponse,
  requireCompletedConnectorAction,
} from "../../connectors/templates/_shared/action-result";

it.each([null, [], "completed", {}, { status: 1 }, { status: "completed", error: 1 }, { status: "completed", display_text: [] }])(
  "rejects malformed action projections %j",
  (value) => expect(() => connectorActionResultResponse(value)).toThrow("Invalid connector action result."),
);

it.each([0, -1, 1.5, Number.MAX_SAFE_INTEGER + 1, {}, null])("rejects an invalid numeric request identity %j", (request_id) => {
  expect(() => connectorActionResultResponse({ status: "running", request_id })).toThrow("Invalid connector action result.");
});

it.each(["running", "approval_pending"])("keeps %s separate from completed action results", (status) => {
  const item = connectorActionResultResponse({ status, request_id: 71 });
  expect(connectorActionPending(item)).toBe(true);
  expect(connectorActionError(item)).toBe("");
  expect(requireCompletedConnectorAction(item)).toBeNull();
  expect(connectorActionRequestID(item)).toBe(71);
});

it("does not mistake an unknown outcome for ordinary pending or completion", () => {
  const item = connectorActionResultResponse({ status: "outcome_unknown", request_id: 71, error: "Reconcile the original request." });
  expect(connectorActionPending(item)).toBe(false);
  expect(() => requireCompletedConnectorAction(item)).toThrow(expect.objectContaining({ actionItem: item }));
});

it("preserves definitive failures and their original action projection", () => {
  const item = connectorActionResultResponse({ status: "failed", error: "failure", output: { code: "remote_error" } });
  expect(connectorActionCode(item)).toBe("remote_error");
  expect(() => requireCompletedConnectorAction(item)).toThrow(expect.objectContaining({ message: "failure", actionItem: item }));
  expect(connectorActionError({ status: "failed", display_text: "details" })).toBe("details");
  expect(connectorActionError(undefined, "fallback")).toBe("fallback");
});

it("returns completed projections unchanged and tolerates missing diagnostic codes", () => {
  const item = connectorActionResultResponse({ status: "completed", request_id: "71", output: { code: "" } });
  expect(requireCompletedConnectorAction(item)).toBe(item);
  expect(connectorActionCode(item)).toBe("");
  expect(connectorActionCode({ output: "not a diagnostic record" })).toBe("");
  expect(connectorActionCode(null)).toBe("");
  expect(() => connectorActionRequestID({ request_id: "invalid" })).toThrow(/missing request_id/);
  expect(() => connectorActionRequestID(undefined)).toThrow(/missing request_id/);
});
