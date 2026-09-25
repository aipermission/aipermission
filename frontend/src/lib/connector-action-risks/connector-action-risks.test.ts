import assert from "node:assert/strict";
import test from "node:test";
import {
  connectorActionRiskDescription,
  connectorActionRiskGroupLabel,
  connectorActionRiskTone,
  normalizeConnectorActionRisk,
} from "../connector-action-risks.ts";

test("unknown connector action risks use the neutral group", () => {
  assert.equal(normalizeConnectorActionRisk("unexpected"), "other");
  assert.equal(connectorActionRiskGroupLabel("unexpected"), "Other operations");
  assert.equal(connectorActionRiskTone("unexpected"), "neutral");
  assert.equal(connectorActionRiskDescription("unexpected", 0), "No uncategorized actions exposed.");
});

test("destructive actions retain a distinct risk tone and count", () => {
  assert.equal(connectorActionRiskTone("destructive"), "bad");
  assert.equal(connectorActionRiskDescription("destructive", 2), "2 destructive actions");
});
