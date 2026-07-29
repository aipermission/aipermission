import assert from "node:assert/strict";
import test from "node:test";
import { preferredDefaultBindings } from "./vault-session-selection.js";

test("preferredDefaultBindings prefers the target project and remains deterministic", () => {
  const defaults = [
    { id: 9, vault_item_id: 12, source_project_id: 8, replace_existing: false },
    { id: 7, vault_item_id: 12, source_project_id: 3, replace_existing: true },
    { id: 4, vault_item_id: 15, source_project_id: 5, replace_existing: false },
    { id: 2, vault_item_id: 15, source_project_id: 4, replace_existing: true },
  ];

  assert.deepEqual(
    preferredDefaultBindings(defaults, 3).map((item) => item.id),
    [7, 2],
  );
  assert.deepEqual(
    preferredDefaultBindings([...defaults].reverse(), 3).map((item) => item.id),
    [7, 2],
  );
});
