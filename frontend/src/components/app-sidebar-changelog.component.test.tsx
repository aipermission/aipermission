import { act, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { renderSidebar } from "../test/fixtures/app-sidebar";

const noteLoad = vi.hoisted(() => {
  let resolve!: () => void;
  const pending = new Promise<void>((done) => {
    resolve = done;
  });
  return { resolve, pending, attempts: 0 };
});
vi.mock("./changelog-entries", async () => {
  noteLoad.attempts++;
  await noteLoad.pending;
  return { ChangelogEntries: () => <p>Loaded canonical notes</p> };
});

it("loads notes only on demand and never reopens a dialog closed while loading", async () => {
  const user = userEvent.setup();
  renderSidebar();
  expect(noteLoad.attempts).toBe(0);
  await user.click(screen.getByRole("button", { name: /Changelog/ }));
  expect(await screen.findByText("Loading changelog...")).toBeVisible();
  expect(noteLoad.attempts).toBe(1);
  await user.click(screen.getByRole("button", { name: "Close dialog" }));
  await act(async () => {
    noteLoad.resolve();
    await noteLoad.pending;
  });
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: /Changelog/ }));
  expect(await screen.findByText("Loaded canonical notes")).toBeVisible();
  expect(noteLoad.attempts).toBe(1);
});
