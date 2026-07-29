import assert from "node:assert/strict";
import test from "node:test";
import { enabledProjectIDsForVisibility } from "./project-scopes.js";

test("project visibility updates one project without changing the others", () => {
  const projects = [
    { project_id: 1, enabled: true },
    { project_id: 2, enabled: false },
    { project_id: 3, enabled: true },
  ];
  assert.deepEqual(enabledProjectIDsForVisibility(projects, 2, true), [1, 2, 3]);
  assert.deepEqual(enabledProjectIDsForVisibility(projects, 1, false), [3]);
});
