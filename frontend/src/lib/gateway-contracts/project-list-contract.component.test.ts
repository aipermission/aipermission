import { describe, expect, it } from "vitest";
import { projectListResponse } from "./project-list-contract.ts";

const project = { id: 1, name: "My Project", slug: "my-project", target_count: 2 };

describe("Project list contract", () => {
  it("projects validated identities and target counters", () => {
    expect(projectListResponse({ items: [{ ...project, extra: "unused" }] })).toEqual([project]);
    expect(projectListResponse({ items: null })).toEqual([]);
  });

  it.each([
    null, [], {}, { items: {} }, { items: [{ ...project, id: "1" }] },
    { items: [{ ...project, id: 0 }] }, { items: [{ ...project, name: {} }] },
    { items: [{ ...project, slug: "" }] }, { items: [{ ...project, target_count: -1 }] },
    { items: [{ ...project, target_count: 0.5 }] },
  ])("rejects malformed list data (%j)", (value) => {
    expect(() => projectListResponse(value)).toThrow("Invalid project list response.");
  });
});
