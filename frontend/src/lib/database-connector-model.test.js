import assert from "node:assert/strict";
import test from "node:test";

import { createDatabaseConnectorModel } from "../connectors/templates/_shared/database-connector-model.js";

function testModel() {
  return createDatabaseConnectorModel({
    kind: "analytics",
    label: "Analytics",
    defaultRiskLabel: "read-only",
    targetDefaults: { name: "analytics", connection_mode: "direct", host: "127.0.0.1", port: 9000, database: "default", transport_target_ref: "" },
    credentialDefaults: { target_id: "", profile_label: "readonly", username: "", password: "", risk_label: "read-only" },
    targetForm: (target) => ({ connection_mode: target.config.connection_mode, host: target.config.host, port: target.config.port, database: target.config.database }),
    targetConfig: (form) => ({ connection_mode: form.connection_mode, host: form.host, port: Number(form.port), database: form.database }),
    targetEndpoint: ({ target }) => `${target.config.host}:${target.config.port}/${target.config.database}`,
  });
}

test("database model factory keeps target and credential behavior connector-scoped", () => {
  const model = testModel();
  assert.equal(model.emptyForm().connector_kind, "analytics");
  assert.equal(model.syncForm({ form: { connector_kind: "analytics", connection_mode: "direct", transport_target_ref: "ssh:1:1" } }).transport_target_ref, "");

  const target = {
    id: 4,
    connector_kind: "analytics",
    name: "warehouse",
    config: { connection_mode: "direct", host: "db.local", port: 9000, database: "events" },
    profiles: [{ id: 8, label: "readonly", kind: "username_password", risk_label: "read-only", public: { username: "reader" } }],
  };
  const form = model.formFromTarget({ target, profile: target.profiles[0] });
  assert.equal(form.profile_id, "8");
  assert.equal(form.username, "reader");
  assert.equal(model.targetEndpoint({ target }), "db.local:9000/events");

  const rows = model.credentialRows({ targets: [target, { ...target, connector_kind: "other" }] });
  assert.equal(rows.length, 1);
  assert.equal(rows[0].row_id, "analytics:4:8");
  assert.deepEqual(rows[0].metadata, ["username: reader", "risk: read-only"]);
});
