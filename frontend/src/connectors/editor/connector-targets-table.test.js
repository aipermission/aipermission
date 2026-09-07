import assert from "node:assert/strict";
import test from "node:test";
import { connectorTargetGroups } from "./connector-target-groups.js";

test("connector target grouping remains project-scoped and searches profiles", () => {
  const projects = [
    { id: 1, name: "My Project", slug: "my-project", target_count: 2 },
    { id: 2, name: "Other", slug: "other", target_count: 1 },
  ];
  const targets = [
    { id: 1, project_id: 1, name: "Primary", connector_kind: "ssh", profiles: [{ label: "admin" }] },
    { id: 2, project_id: 1, name: "Data", connector_kind: "postgres", profiles: [{ label: "readonly" }] },
    { id: 3, project_id: 2, name: "Cache", connector_kind: "redis", profiles: [] },
  ];
  assert.deepEqual(
    connectorTargetGroups(projects, targets, "readonly").map((group) => [group.project.id, group.targets.map((target) => target.id)]),
    [[1, [2]]],
  );
  assert.deepEqual(
    connectorTargetGroups(projects, targets, "my-project").map((group) => [group.project.id, group.targets.map((target) => target.id)]),
    [[1, [1, 2]]],
  );
  assert.equal(connectorTargetGroups(projects, targets, "missing").length, 0);
});
