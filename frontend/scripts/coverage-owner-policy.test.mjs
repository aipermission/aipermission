import assert from "node:assert/strict";
import test from "node:test";

import { isBehaviorOwner } from "./coverage-owner-policy.mjs";

test("treats production modules as owners by default", () => {
  const owners = [
    "src/pages/history.jsx",
    "src/components/console/use-console-session-coordinator.js",
    "src/components/console/connector-action-approval-dialog.jsx",
    "src/components/file-transfer/file-transfer-actions.js",
    "src/connectors/templates/redis/console.jsx",
    "src/connectors/templates/redis/key-browser.jsx",
    "src/components/ui/button.jsx",
    "src/lib/local-action-retry.js",
  ];
  const exclusions = [
    "src/lib/release.generated.json",
    "src/lib/mcp-client-catalog.js",
    "src/pages/history.component.test.jsx",
    "src/test/setup.js",
  ];

  owners.forEach((file) => assert.equal(isBehaviorOwner(file), true, file));
  exclusions.forEach((file) => assert.equal(isBehaviorOwner(file), false, file));
});
