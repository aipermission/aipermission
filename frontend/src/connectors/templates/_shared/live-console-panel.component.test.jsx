import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { LiveConsolePanel } from "./live-console-panel";

const baseProps = {
  subject: "container",
  subjectRef: "api-1",
  emptyMessage: "Select a container.",
  selectedRuntimeTarget: { id: 1 },
  sessionLive: false,
  pending: false,
  theme: "dark",
  mutedClass: "text-stone-400",
  borderClass: "border-stone-700",
};

describe("LiveConsolePanel", () => {
  it("distinguishes empty, connecting, and live console states", () => {
    const { rerender } = render(<LiveConsolePanel {...baseProps} subject="" />);
    expect(screen.getByText("Select a container.")).toBeInTheDocument();

    rerender(<LiveConsolePanel {...baseProps} pending />);
    expect(screen.getByText("Connecting container console")).toBeInTheDocument();
    expect(screen.queryByText("No active container console")).not.toBeInTheDocument();

    rerender(
      <LiveConsolePanel {...baseProps} sessionLive>
        <div>terminal surface</div>
      </LiveConsolePanel>,
    );
    expect(screen.getByText("terminal surface")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "End" })).toBeInTheDocument();
  });

  it("keeps lifecycle callbacks and unavailable runtime state explicit", async () => {
    const user = userEvent.setup();
    const onStart = vi.fn();
    const onEnd = vi.fn();
    const { rerender } = render(<LiveConsolePanel {...baseProps} onStart={onStart} onEnd={onEnd} />);

    await user.click(screen.getByRole("button", { name: "Start Container Console" }));
    expect(onStart).toHaveBeenCalledOnce();

    rerender(<LiveConsolePanel {...baseProps} selectedRuntimeTarget={null} onStart={onStart} onEnd={onEnd} />);
    expect(screen.getByRole("button", { name: "Start Container Console" })).toBeDisabled();
    expect(screen.getByText(/does not have a live runtime surface/i)).toBeInTheDocument();

    rerender(<LiveConsolePanel {...baseProps} sessionLive onStart={onStart} onEnd={onEnd} />);
    await user.click(screen.getByRole("button", { name: "End" }));
    expect(onEnd).toHaveBeenCalledOnce();
  });
});
