import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ConsoleRecoveryPanel } from "./console-recovery-panel.tsx";

const now = Date.parse("2026-09-26T12:00:00Z");
const request = { created_at: "2026-09-26T11:59:00Z", action_name: "exec", input: { command: "uptime\nsecond line" }, token_name: "Agent" };

describe("ConsoleRecoveryPanel", () => {
  it("shows a bounded preview and elapsed time and sends restart through its owner", () => {
    const onRestart = vi.fn();
    render(<ConsoleRecoveryPanel request={request} now={now} theme="dark" action={{ state: "idle" }} onRestart={onRestart} />);
    expect(screen.getByText("Connector action running")).toBeInTheDocument();
    expect(screen.getByText("1m 0s")).toBeInTheDocument();
    expect(screen.getByText("uptime")).toBeInTheDocument();
    expect(screen.queryByText("second line")).not.toBeInTheDocument();
    expect(screen.getByText(/Looks stuck/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Restart" }));
    expect(onRestart).toHaveBeenCalledOnce();
  });

  it("disables duplicate restarts and displays the owned failure", () => {
    render(<ConsoleRecoveryPanel request={request} now={now} theme="light" action={{ state: "running", error: "Unavailable" }} onRestart={vi.fn()} />);
    expect(screen.getByRole("button", { name: "Restarting..." })).toBeDisabled();
    expect(screen.getByText("Unavailable")).toBeInTheDocument();
  });

  it.each([["manual", "Manual command running"], ["mcp", "AI command running"], ["other", "Command running"]])("labels %s requests without inventing a connector action", (source, label) => {
    render(<ConsoleRecoveryPanel request={{ created_at: "2026-09-26T12:00:00Z", source, command: "x".repeat(120) }} now={now} theme="light" action={{ state: "idle" }} onRestart={vi.fn()} />);
    expect(screen.getByText(label)).toBeInTheDocument();
    expect(screen.getByText(`${"x".repeat(87)}...`)).toBeInTheDocument();
    expect(screen.queryByText(/Looks stuck/)).not.toBeInTheDocument();
  });
});
