import { describe, expect, it } from "vitest";
import { consoleTargetRows, defaultConsoleTargetRef, groupConsoleTargetsByProject } from "./console-target-sidebar";

const admin = {
  connector_kind: "postgres",
  target_id: 7,
  profile_id: 11,
  ref: "postgres:7:11",
  project_id: 3,
  project_name: "My Project",
};
const readonly = { ...admin, profile_id: 12, ref: "postgres:7:12" };
const cache = {
  connector_kind: "redis",
  target_id: 8,
  profile_id: 13,
  ref: "redis:8:13",
  project_id: null,
  project_name: "",
  runtime_id: 9,
};

describe("console target navigation", () => {
  it("keeps one row per connector and preserves the preferred profile", () => {
    const rows = consoleTargetRows([admin, readonly, cache], null, { "postgres:7": 12 });

    expect(rows).toHaveLength(2);
    expect(rows[0].ref).toBe("postgres:7:12");
    expect(rows[1].ref).toBe("redis:8:13");
  });

  it("groups unassigned connectors and prioritizes pending or unread targets", () => {
    const groups = groupConsoleTargetsByProject([admin, cache]);
    expect(groups.map((group) => group.name)).toEqual(["My Project", "Ungrouped"]);
    expect(defaultConsoleTargetRef([admin, cache], [], [{ target_ref: cache.ref }])).toBe(cache.ref);
    expect(defaultConsoleTargetRef([admin, cache], [{ runtime_id: 9 }], [])).toBe(cache.ref);
  });
});
