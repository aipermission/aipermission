import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { NoLiveSession } from "./no-live-session";

it("describes a missing session and starts a new one", async () => {
  const onNewSession = vi.fn();
  render(<NoLiveSession target={{ name: "Example host" }} onNewSession={onNewSession} />);
  expect(screen.getByText("Start a shell session before sending commands to Example host.")).toBeVisible();
  await userEvent.click(screen.getByRole("button", { name: "New Session" }));
  expect(onNewSession).toHaveBeenCalledOnce();
});

it("shows the last closed session with a resilient timestamp", () => {
  render(
    <NoLiveSession
      target={{ name: "Example host" }}
      lastSession={{ status: "closed", updated_at: "not-a-date" }}
      onNewSession={vi.fn()}
      theme="light"
    />,
  );
  expect(screen.getByText("The last Example host session is closed and cannot accept input anymore.")).toBeVisible();
  expect(screen.getByText("Last session: not-a-date")).toBeVisible();
});
