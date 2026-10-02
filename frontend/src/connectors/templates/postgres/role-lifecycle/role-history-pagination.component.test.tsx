import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { roleHistoryCursorPageFixture } from "../../../../test/postgres/role-history-fixtures.test";
import { roleHistoryPage } from "./role-history-contract";
import { RoleHistoryDialog } from "./role-history-dialog";
import { useRoleHistory } from "./use-role-history";

vi.mock("./use-role-history", () => ({ useRoleHistory: vi.fn() }));

it.each([64, 65, 66])("keeps navigation wired for a full page at page %s", (pageNumber) => {
  const after = (pageNumber - 1) * 64;
  const raw = roleHistoryCursorPageFixture(7, after + 1, 64, true);
  const history: ReturnType<typeof useRoleHistory> = {
    targetID: 7,
    state: "ready",
    page: roleHistoryPage(raw, 7, String(after)),
    error: "",
    cursors: Array.from({ length: 64 }, (_, index) => String((pageNumber - 64 + index) * 64)),
    pageOffset: pageNumber - 64,
    pageNumber,
    workspaceChanged: false,
    busy: false,
    notice: "",
    canFirst: true,
    canPrevious: true,
    canNext: true,
    refresh: vi.fn(async () => {}),
    first: vi.fn(async () => {}),
    previous: vi.fn(async () => {}),
    next: vi.fn(async () => {}),
    reconcile: vi.fn(async () => {}),
    cleanup: vi.fn(async () => {}),
  };
  vi.mocked(useRoleHistory).mockReturnValue(history);
  render(<RoleHistoryDialog target={{ id: 7 }} onClose={vi.fn()} />);
  expect(screen.getByText(`Page ${pageNumber}`)).toBeInTheDocument();
  expect(screen.getAllByRole("listitem")).toHaveLength(64);
  expect(screen.queryByText(/limit reached/)).not.toBeInTheDocument();
  for (const [label, callback] of [
    ["Next", history.next],
    ["Previous", history.previous],
    ["First", history.first],
  ] as const) {
    const button = screen.getByRole("button", { name: label });
    expect(button).toBeEnabled();
    fireEvent.click(button);
    expect(callback).toHaveBeenCalledTimes(1);
  }
});
