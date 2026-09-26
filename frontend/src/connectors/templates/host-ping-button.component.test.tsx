import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiPost } from "../../lib/api";
import { HostPingButton } from "./host-ping-button";

vi.mock("../../lib/api", () => ({ apiPost: vi.fn() }));

beforeEach(() => {
  vi.mocked(apiPost).mockReset();
});

it("keeps a dismissed ping dialog closed after a stale response resolves", async () => {
  const user = userEvent.setup();
  let resolvePing: (_value: unknown) => void = () => {
    throw new Error("Ping request was not dispatched");
  };
  vi.mocked(apiPost).mockImplementationOnce(
    () =>
      new Promise<unknown>((resolve) => {
        resolvePing = resolve;
      }),
  );
  render(<HostPingButton host="db.internal" port={5432} />);

  await user.click(screen.getByRole("button", { name: "Ping host" }));
  expect(screen.getByRole("dialog", { name: "Host reachability" })).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Close" }));
  expect(screen.queryByRole("dialog", { name: "Host reachability" })).not.toBeInTheDocument();

  resolvePing({ ok: true, sent: 4, received: 4, duration_ms: 8, mode: "direct", attempts: [] });
  await Promise.resolve();

  expect(screen.queryByRole("dialog", { name: "Host reachability" })).not.toBeInTheDocument();
  const options = vi.mocked(apiPost).mock.calls[0][2];
  const signal = options && "signal" in options ? options.signal : undefined;
  expect(signal).toBeInstanceOf(AbortSignal);
  if (!(signal instanceof AbortSignal)) throw new Error("Ping request must provide an abort signal");
  expect(signal.aborted).toBe(true);
});

it("requires a project and transport selection before checking an indirect host", () => {
  const { rerender } = render(<HostPingButton host="db.internal" port={5432} mode="over_ssh" />);
  expect(screen.getByRole("button", { name: "Ping host" })).toBeDisabled();
  rerender(<HostPingButton host="db.internal" port={5432} mode="over_ssh" transportTargetRef="ssh:1:2" />);
  expect(screen.getByRole("button", { name: "Ping host" })).toHaveAttribute("title", "Select a project before testing this transport.");
  expect(screen.getByRole("button", { name: "Ping host" })).toBeDisabled();
  rerender(<HostPingButton host="db.internal" port={5432} mode="over_ssh" transportTargetRef="ssh:1:2" projectID="3" />);
  expect(screen.getByRole("button", { name: "Ping host" })).toBeEnabled();
  expect(apiPost).not.toHaveBeenCalled();
});

it("renders per-attempt reachability and preserves transport routing", async () => {
  const user = userEvent.setup();
  vi.mocked(apiPost).mockResolvedValueOnce({
    ok: false,
    received: 1,
    sent: 4,
    duration_ms: 20,
    mode: "over_ssh",
    message: "Partial reachability",
    attempts: [
      { attempt: 1, ok: true, duration_ms: 2 },
      { attempt: 2, ok: false, duration_ms: 18, error: "Timed out" },
    ],
  });
  render(<HostPingButton host="db.internal" port="5432" mode="over_ssh" transportTargetRef="ssh:1:2" projectID="3" />);
  await user.click(screen.getByRole("button", { name: "Ping host" }));
  expect(await screen.findByText("Partial reachability")).toBeVisible();
  expect(screen.getByText("1/4 reachable")).toBeVisible();
  expect(screen.getByText("Timed out")).toBeVisible();
  expect(apiPost).toHaveBeenCalledWith(
    "/api/connector-targets/ping",
    {
      project_id: 3,
      host: "db.internal",
      port: 5432,
      mode: "over_ssh",
      transport_target_ref: "ssh:1:2",
      attempts: 4,
    },
    expect.objectContaining({ signal: expect.any(AbortSignal) }),
  );
});

it("reports an unknown rejection without losing the dialog", async () => {
  vi.mocked(apiPost).mockRejectedValueOnce(undefined);
  render(<HostPingButton host="db.internal" port={5432} />);
  await userEvent.setup().click(screen.getByRole("button", { name: "Ping host" }));
  expect(await screen.findByText("Ping failed.")).toBeVisible();
});
