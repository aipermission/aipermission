import { expect, it } from "vitest";
import { connectorConnectionTestResponse, savedConnectorTargetResponse } from "./connector-management-contracts";
import { connectorActionResultResponse } from "../../connectors/templates/_shared/action-result";
import { pingResultResponse } from "../../connectors/templates/ping-result-contract";
import { provisionResultResponse } from "../../connectors/templates/postgres/provision-result-contract";

it("validates connection success and every consumed error field", () => {
  const valid = { ok: false, message: "unreachable", stdout: "", stderr: "timeout", details: {} };
  expect(connectorConnectionTestResponse(valid)).toBe(valid);
  for (const value of [null, [], {}, { ok: "yes" }, { ok: true, stderr: {} }, { ok: true, stdout: 7 }, { ok: true, message: [] }])
    expect(() => connectorConnectionTestResponse(value)).toThrow("Invalid connector management response");
});

it("requires real saved resource identifiers before selecting its first profile", () => {
  const valid = { id: 3, profiles: [{ id: 4, label: "main" }] };
  expect(savedConnectorTargetResponse(valid)).toBe(valid);
  expect(savedConnectorTargetResponse({ id: 3 })).toEqual({ id: 3 });
  for (const value of [
    {},
    { id: 0 },
    { id: Number.MAX_SAFE_INTEGER + 1 },
    { id: 1, profiles: {} },
    { id: 1, profiles: [null] },
    { id: 1, profiles: [{ id: "4" }] },
  ])
    expect(() => savedConnectorTargetResponse(value)).toThrow();
});

it("does not let malformed SQL action status or errors reach the console", () => {
  expect(connectorActionResultResponse({ status: "completed", request_id: 3, output: { rows: [] } })).toMatchObject({ request_id: 3 });
  for (const value of [
    null,
    {},
    { status: 3 },
    { status: "failed", error: {} },
    { status: "running", display_text: [] },
    { status: "completed", request_id: -1 },
  ])
    expect(() => connectorActionResultResponse(value)).toThrow();
});

it("validates reachability attempts and timing before rendering", () => {
  expect(
    pingResultResponse({
      ok: true,
      sent: 4,
      received: 4,
      duration_ms: 20,
      mode: "direct",
      attempts: [{ attempt: 1, ok: true, duration_ms: 5 }],
    }),
  ).toMatchObject({ received: 4 });
  const base = { ok: true, sent: 4, received: 4, duration_ms: 20, mode: "direct" };
  for (const value of [
    { ...base, sent: {} },
    { ...base, duration_ms: -1 },
    { ...base, attempts: {} },
    { ...base, attempts: [{ attempt: 1, ok: "yes", duration_ms: 5 }] },
  ])
    expect(() => pingResultResponse(value)).toThrow();
});

it("validates managed profile labels and SQL provisioning display text", () => {
  expect(provisionResultResponse({ profile: { id: 3, label: "reader" }, result: { display_text: "Created" } })).toMatchObject({
    profile: { id: 3 },
  });
  for (const value of [null, [], { profile: [] }, { profile: { id: 0 } }, { profile: { label: {} } }, { result: { display_text: {} } }])
    expect(() => provisionResultResponse(value)).toThrow();
});
