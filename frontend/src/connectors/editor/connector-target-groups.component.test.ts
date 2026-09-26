import { describe, expect, it } from "vitest";

import { connectorTargetGroups } from "./connector-target-groups";

describe("connector target groups", () => {
  const projects = [
    { id: 1, name: "Application", slug: "application", target_count: 2 },
    { id: 2, name: "Operations", slug: "operations", target_count: 1 },
    { id: 3, name: "Archived", slug: "archived", target_count: 0 },
  ];
  const targets = [
    { project_id: 1, name: "Primary", connector_kind: "ssh", profiles: [{ label: "admin" }] },
    { project_id: 1, name: "Data", connector_kind: "postgres", profiles: [{ label: "readonly" }] },
    { project_id: 2, name: "Cache", connector_kind: "redis", profiles: [{ label: "readonly" }] },
  ];

  it("keeps only populated projects and their own targets without a search", () => {
    expect(connectorTargetGroups(projects, targets, " ").map(({ project, targets: groupTargets }) => [project.id, groupTargets.length])).toEqual([
      [1, 2],
      [2, 1],
    ]);
  });

  it("matches connector kind without returning another project's target", () => {
    expect(connectorTargetGroups(projects, targets, "POSTGRES").map(({ project, targets: groupTargets }) => [project.id, groupTargets[0].name])).toEqual([
      [1, "Data"],
    ]);
  });
});
