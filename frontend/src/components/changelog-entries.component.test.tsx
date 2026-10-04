import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import { ChangelogEntries } from "./changelog-entries";
import release from "../lib/release.generated.json" with { type: "json" };
import { appVersion } from "../lib/release";

it("renders every canonical note without losing version metadata", () => {
  render(<ChangelogEntries />);
  expect(appVersion).toBe(release.version);
  for (const entry of release.entries) {
    expect(screen.getByRole("heading", { name: entry.version })).toBeVisible();
    expect(screen.getAllByText(entry.label, { exact: true }).length).toBeGreaterThan(0);
    for (const section of entry.sections) {
      for (const item of section.items) expect(screen.getAllByText(item, { exact: true }).length).toBeGreaterThan(0);
    }
  }
});
