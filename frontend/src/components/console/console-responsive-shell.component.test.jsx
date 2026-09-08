import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { ConsoleResponsiveShell } from "./console-responsive-shell";

function TargetPanel({ onSelect }) {
  return (
    <div>
      <button type="button" onClick={() => onSelect("target:1")}>
        Select target
      </button>
    </div>
  );
}

function TokenPanel() {
  return <div>Token rules</div>;
}

it("opens narrow side panels and closes targets after selection", async () => {
  const user = userEvent.setup();
  const onSelect = vi.fn();
  render(
    <ConsoleResponsiveShell
      targetsCompact={false}
      tokensCompact={false}
      targetSidebar={<TargetPanel onSelect={onSelect} />}
      workspace={<div>Workspace content</div>}
      tokenPanel={<TokenPanel />}
    />,
  );

  expect(screen.getByText("Workspace content")).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Connectors" }));
  await user.click(screen.getByRole("button", { name: "Select target" }));
  expect(onSelect).toHaveBeenCalledWith("target:1");
  expect(screen.queryByRole("button", { name: "Select target" })).not.toBeInTheDocument();

  await user.click(screen.getByRole("button", { name: "Tokens" }));
  expect(screen.getByText("Token rules")).toBeVisible();
});
