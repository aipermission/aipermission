import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { ConnectorActivityDialog } from "./connector-activity-dialog";

const approvals = {
  state: "ready",
  data: [
    {
      id: 1,
      target_name: "Primary database",
      target_ref: "postgres:1:1",
      connector_kind: "postgres",
      profile_label: "admin",
      action_name: "query_readonly",
      status: "completed",
      reason: "Inspect rows",
      input: { query: "select 1" },
      output: { rows: [{ value: 1 }] },
      created_at: "2026-01-01T10:00:00Z",
    },
    {
      id: 2,
      target_ref: "redis:2:2",
      connector_kind: "redis",
      action_name: "get_key",
      status: "failed",
      error: "Read failed",
      input: "key",
      display_text: "No value",
    },
  ],
};

it("selects structured activity and refreshes the stream", async () => {
  const user = userEvent.setup();
  const onRefresh = vi.fn();
  render(<ConnectorActivityDialog open approvals={approvals} onRefresh={onRefresh} onClose={vi.fn()} />);

  expect(screen.getByText(/Reason: Inspect rows/)).toBeVisible();
  await user.click(screen.getByRole("button", { name: /redis:2:2/ }));
  expect(screen.getByText("Read failed")).toBeVisible();
  expect(screen.getByText("No value")).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Refresh connector activity" }));
  expect(onRefresh).toHaveBeenCalledOnce();
});

it("renders a stable empty activity state while loading", () => {
  render(<ConnectorActivityDialog open approvals={{ state: "loading", data: [] }} onRefresh={vi.fn()} onClose={vi.fn()} />);
  expect(screen.getByText("No connector activity yet.")).toBeVisible();
  expect(screen.getByText("Select a connector request to inspect input and output.")).toBeVisible();
});
