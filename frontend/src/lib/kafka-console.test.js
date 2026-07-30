import assert from "node:assert/strict";
import test from "node:test";
import { connectorActionError, detailMatchesSelection, requestIsCurrent } from "../connectors/templates/kafka/console-helpers.js";
import { credentialPayload } from "../connectors/templates/kafka/model-helpers.js";

test("Kafka action helpers surface failed HTTP 200 responses", () => {
  assert.equal(connectorActionError({ status: "failed", error: "broker denied access" }), "broker denied access");
  assert.equal(connectorActionError({ status: "completed", output: {} }), "");
});

test("Kafka action helpers reject stale target and channel responses", () => {
  const versions = new Map([["detail", 2]]);
  assert.equal(requestIsCurrent(versions, "detail", 2, "kafka:1:1", "kafka:1:1"), true);
  assert.equal(requestIsCurrent(versions, "detail", 1, "kafka:1:1", "kafka:1:1"), false);
  assert.equal(requestIsCurrent(versions, "detail", 2, "kafka:1:1", "kafka:2:2"), false);
});

test("Kafka detail helpers bind responses to the selected view and item", () => {
  assert.equal(detailMatchesSelection("topics:events", "topics", "events"), true);
  assert.equal(detailMatchesSelection("topics:events", "groups", "events"), false);
  assert.equal(detailMatchesSelection("topics:events", "topics", "orders"), false);
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
