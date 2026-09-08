import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { apiPost } from "../../lib/api";
import { HostPingButton } from "./host-ping-button";

vi.mock("../../lib/api", () => ({ apiPost: vi.fn() }));

it("keeps a dismissed ping dialog closed after a stale response resolves", async () => {
  const user = userEvent.setup();
  let resolvePing;
  apiPost.mockImplementationOnce(() => new Promise((resolve) => (resolvePing = resolve)));
  render(<HostPingButton host="db.internal" port={5432} />);

  await user.click(screen.getByRole("button", { name: "Ping host" }));
  expect(screen.getByRole("dialog", { name: "Host reachability" })).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Close" }));
  expect(screen.queryByRole("dialog", { name: "Host reachability" })).not.toBeInTheDocument();

  resolvePing({ ok: true, sent: 4, received: 4, duration_ms: 8, mode: "direct", attempts: [] });
  await Promise.resolve();

  expect(screen.queryByRole("dialog", { name: "Host reachability" })).not.toBeInTheDocument();
  expect(apiPost.mock.calls[0][2].signal).toBeInstanceOf(AbortSignal);
  expect(apiPost.mock.calls[0][2].signal.aborted).toBe(true);
});
