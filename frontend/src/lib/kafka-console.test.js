import assert from "node:assert/strict";
import test from "node:test";
import {
  connectorActionError,
  connectorActionPending,
  requireCompletedConnectorAction,
} from "../connectors/templates/_shared/action-result.js";
import {
  actionableOffsetPartitions,
  detailMatchesSelection,
  offsetSelectionValue,
  parseOffsetSelection,
} from "../connectors/templates/kafka/console-helpers.js";
import { credentialPayload } from "../connectors/templates/kafka/model-helpers.js";

test("Kafka action helpers surface failed HTTP 200 responses", () => {
  assert.equal(connectorActionError({ status: "failed", error: "broker denied access" }), "broker denied access");
  assert.equal(connectorActionError({ status: "completed", output: {} }), "");
  assert.equal(connectorActionError({ status: "running", display_text: "still running" }), "");
  assert.equal(connectorActionPending({ status: "running" }), true);
  assert.equal(connectorActionError(null, "missing result"), "missing result");
});

test("completed action guard rejects failed results and withholds pending results", () => {
  assert.equal(requireCompletedConnectorAction({ status: "completed", output: { ok: true } }).output.ok, true);
  assert.equal(requireCompletedConnectorAction({ status: "approval_pending" }), null);
  assert.throws(() => requireCompletedConnectorAction({ status: "blocked", error: "permission blocked" }), /permission blocked/);
});

test("Kafka detail actions stay bound to the selected view and item", () => {
  assert.equal(detailMatchesSelection("topics:orders", "topics", "orders"), true);
  assert.equal(detailMatchesSelection("topics:orders", "topics", "payments"), false);
  assert.equal(detailMatchesSelection("topics:orders", "groups", "orders"), false);
  assert.equal(detailMatchesSelection("", "topics", "orders"), false);
});

test("Kafka offset controls preserve exact topic and partition selections", () => {
  const value = offsetSelectionValue({ topic: "orders/priority", partition: 12 });
  assert.deepEqual(parseOffsetSelection(value), { topic: "orders/priority", partition: 12 });
  assert.equal(parseOffsetSelection('["",1]'), null);
  assert.equal(parseOffsetSelection('["orders",-1]'), null);
  assert.equal(parseOffsetSelection("not-json"), null);
});

test("Kafka offset controls exclude broker error rows", () => {
  const partitions = actionableOffsetPartitions([
    { topic: "orders", partition: 0, committed_offset: "12", end_offset: "20" },
    { topic: "orders", partition: 1, error: "not leader" },
    { topic: "", partition: 2, committed_offset: "4", end_offset: "5" },
  ]);
  assert.deepEqual(partitions, [{ topic: "orders", partition: 0, committed_offset: "12", end_offset: "20" }]);
});

test("Kafka credential edits preserve an existing SASL password", () => {
  const form = {
    profile_label: "monitor",
    sasl_mechanism: "scram_sha_512",
    existing_sasl_mechanism: "scram_sha_512",
    username: "reader",
    password: "",
    risk_label: "stream read",
  };
  assert.equal(credentialPayload(form).secret, undefined);
});

test("Kafka credential payload requires a password when SASL is enabled", () => {
  const form = {
    profile_label: "monitor",
    sasl_mechanism: "plain",
    existing_sasl_mechanism: "none",
    username: "reader",
    password: "",
    risk_label: "stream read",
  };
  assert.throws(() => credentialPayload(form), /Password is required/);
  assert.deepEqual(credentialPayload({ ...form, password: "test-password" }).secret, { password: "test-password" });
});
