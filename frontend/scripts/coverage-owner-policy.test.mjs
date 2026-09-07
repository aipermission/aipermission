import assert from "node:assert/strict";
import test from "node:test";

import { isBehaviorOwner } from "./coverage-owner-policy.mjs";

test("discovers orchestration owners without treating every presenter as one", () => {
  const owners = [
    "src/pages/history.jsx",
    "src/components/console/use-console-session-coordinator.js",
    "src/components/console/connector-action-approval-dialog.jsx",
    "src/components/file-transfer/file-transfer-actions.js",
    "src/connectors/templates/redis/console.jsx",
  ];
  const presenters = [
    "src/components/ui/button.jsx",
    "src/connectors/templates/redis/key-browser.jsx",
    "src/lib/release.generated.json",
    "src/pages/history.component.test.jsx",
  ];

  owners.forEach((file) => assert.equal(isBehaviorOwner(file), true, file));
  presenters.forEach((file) => assert.equal(isBehaviorOwner(file), false, file));
});
