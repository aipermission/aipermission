import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(path.resolve("src/components/app-shell.jsx"), "utf8");

describe("AppShell transfer wiring", () => {
  it.each(["approve", "cancel", "decline", "pause", "resume"])("routes %s through the matching action callback", (action) => {
    const prop = `on${action[0].toUpperCase()}${action.slice(1)}`;
    expect(source).toMatch(new RegExp(`${prop}=\\{transferBatchActions\\.${action}\\}`));
  });

  it("applies authoritative action responses before refreshing transfer batches", () => {
    expect(source).toMatch(/applyResult: applyFileTransferBatch/);
    expect(source).toMatch(/refresh: loadFileTransferBatches/);
  });
});
