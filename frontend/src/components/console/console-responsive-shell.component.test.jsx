import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { ConsoleResponsiveShell } from "./console-responsive-shell";

const media = vi.hoisted(() => ({ wide: false }));
vi.mock("../../lib/use-media-query", () => ({ useMediaQuery: () => media.wide }));

beforeEach(() => {
  media.wide = false;
});

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
  await user.click(screen.getByRole("button", { name: "Close drawer" }));
  expect(screen.queryByText("Token rules")).not.toBeInTheDocument();

  await user.click(screen.getByRole("button", { name: "Connectors" }));
  await user.click(screen.getByRole("button", { name: "Close drawer" }));
  expect(screen.queryByRole("button", { name: "Select target" })).not.toBeInTheDocument();
});

it("renders both compact-capable side panels at wide width", () => {
  media.wide = true;
  render(
    <ConsoleResponsiveShell
      targetsCompact
      tokensCompact
      targetSidebar={<TargetPanel onSelect={vi.fn()} onCompactChange={vi.fn()} />}
      workspace={<div>Wide workspace</div>}
      tokenPanel={<TokenPanel onToggleCompact={vi.fn()} />}
      dialogs={<div>Dialogs</div>}
    />,
  );

  expect(screen.getByRole("button", { name: "Collapse connectors" })).toBeVisible();
  expect(screen.getByRole("button", { name: "Collapse tokens" })).toBeVisible();
  expect(screen.queryByRole("button", { name: "Connectors" })).not.toBeInTheDocument();
});
