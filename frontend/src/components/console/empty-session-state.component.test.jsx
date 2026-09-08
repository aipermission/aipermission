import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { EmptySessionState } from "./empty-session-state";

it("renders connector-owned session text and starts from a native button", async () => {
  const onStart = vi.fn();
  const user = userEvent.setup();
  render(
    <EmptySessionState
      title="No active Example session"
      description="Start an Example session."
      detail="Last session: yesterday"
      onStart={onStart}
      theme="light"
    />,
  );

  expect(screen.getByRole("heading", { name: "No active Example session" })).toBeVisible();
  expect(screen.getByText("Last session: yesterday")).toBeVisible();
  await user.click(screen.getByRole("button", { name: "New Session" }));
  expect(onStart).toHaveBeenCalledOnce();
});

it("supports the dark empty state without optional detail", () => {
  render(<EmptySessionState title="Closed" description="Open it." onStart={vi.fn()} buttonLabel="Reconnect" />);
  expect(screen.getByRole("button", { name: "Reconnect" })).toBeVisible();
  expect(screen.queryByText(/Last session:/)).not.toBeInTheDocument();
});
