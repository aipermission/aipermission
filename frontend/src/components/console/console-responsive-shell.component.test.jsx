import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { ConsoleResponsiveShell } from "./console-responsive-shell";

function TargetPanel({ onSelect, onCompactChange }) {
  return (
    <div>
      <button type="button" onClick={() => onSelect("target:1")}>
        Select target
      </button>
      {onCompactChange ? <button type="button">Collapse connectors</button> : null}
    </div>
  );
}

function TokenPanel({ onToggleCompact }) {
  return (
    <div>
      Token rules
      {onToggleCompact ? <button type="button">Collapse tokens</button> : null}
    </div>
  );
}

it("opens narrow side panels and closes targets after selection", async () => {
  const user = userEvent.setup();
  const onSelect = vi.fn();
  render(
    <ConsoleResponsiveShell
      targetsCompact={false}
      tokensCompact={false}
      targetSidebar={<TargetPanel onSelect={onSelect} onCompactChange={vi.fn()} />}
      workspace={<div>Workspace content</div>}
      tokenPanel={<TokenPanel onToggleCompact={vi.fn()} />}
    />,
  );

  expect(screen.getByText("Workspace content")).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Connectors" }));
  expect(screen.queryByRole("button", { name: "Collapse connectors" })).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Select target" }));
  expect(onSelect).toHaveBeenCalledWith("target:1");
  expect(screen.queryByRole("button", { name: "Select target" })).not.toBeInTheDocument();

  await user.click(screen.getByRole("button", { name: "Tokens" }));
  expect(screen.getByText("Token rules")).toBeVisible();
  expect(screen.queryByRole("button", { name: "Collapse tokens" })).not.toBeInTheDocument();
});
