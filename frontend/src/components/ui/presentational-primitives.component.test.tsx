import { createRef } from "react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { Card, CardContent, CardTitle } from "./card";
import { PaginationBar } from "./pagination-bar";
import { ProgressBar } from "./progress-bar";
import { TerminalBlock } from "./terminal-block";

describe("typed presentational primitives", () => {
  it("preserves section semantics, bounded progress, and terminal refs", () => {
    const outputRef = createRef<HTMLPreElement>();
    render(
      <Card aria-label="Summary">
        <CardTitle>Summary</CardTitle>
        <CardContent>
          <ProgressBar value={125} active />
          <TerminalBlock ref={outputRef} surface="log">
            ready
          </TerminalBlock>
        </CardContent>
      </Card>,
    );
    expect(screen.getByRole("region", { name: "Summary" })).toBeVisible();
    expect(screen.getByRole("progressbar")).toHaveValue(100);
    expect(outputRef.current).toHaveTextContent("ready");
    expect(outputRef.current).toHaveClass("terminal-log-surface");
  });

  it("keeps page commands disabled when the matching direction is unavailable", async () => {
    const previous = vi.fn();
    const next = vi.fn();
    const user = userEvent.setup();
    render(<PaginationBar start={1} end={20} total={40} hasPrevious={false} hasNext onPrevious={previous} onNext={next} />);
    expect(screen.getByRole("button", { name: "Previous" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "Next" }));
    expect(previous).not.toHaveBeenCalled();
    expect(next).toHaveBeenCalledOnce();
  });
});
