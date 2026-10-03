import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { ConnectorRuleButtons } from "../../components/connectors/connector-rule-button";
import {
  ActionPermissionCard,
  PermissionModeTabs,
  PermissionMutationError,
  PermissionRuleGroup,
  ProfileSelect,
} from "../../components/console/connector-token-permission-controls";

it("uses one rule selector with exact values and keeps the submitted selection during saving", async () => {
  const user = userEvent.setup();
  const onSetRule = vi.fn();
  const view = render(<ConnectorRuleButtons rule="" onSetRule={onSetRule} />);
  for (const [label, value] of [
    ["Disabled", ""],
    ["Blocked", "blocked"],
    ["Prompt", "approval_required"],
    ["Always", "always_run"],
  ]) {
    await user.click(screen.getByRole("button", { name: label }));
    expect(onSetRule).toHaveBeenLastCalledWith(value);
  }
  view.rerender(<ConnectorRuleButtons rule="always_run" saving onSetRule={onSetRule} />);
  expect(screen.getByRole("button", { name: "Always" })).toHaveClass("permission-button-active");
  expect(screen.getAllByRole("button").every((button) => button.hasAttribute("disabled"))).toBe(true);
  view.rerender(<ConnectorRuleButtons rule="always_run" disabled onSetRule={onSetRule} />);
  expect(screen.getByRole("button", { name: "Always" })).not.toHaveClass("permission-button-active");
});

it("preserves optional controls and mixed-state presentation after selector reuse", async () => {
  const user = userEvent.setup();
  const onChange = vi.fn();
  const onSetRule = vi.fn();
  const view = render(<ProfileSelect profiles={[{ connector_kind: "fixture", target_id: 1, profile_id: 2 }]} onChange={onChange} />);
  expect(screen.getByRole("option", { name: "Profile 2" })).toBeVisible();
  view.rerender(<PermissionModeTabs value="basic" onChange={onChange} />);
  await user.click(screen.getByRole("button", { name: "Advanced" }));
  expect(onChange).toHaveBeenLastCalledWith("advanced");
  view.rerender(<PermissionRuleGroup title="Operations" description="Rules" rule="mixed" saving={false} onSetRule={onSetRule} />);
  expect(screen.getByText("mixed")).toBeVisible();
  view.rerender(
    <ActionPermissionCard
      action={{ name: "read", description: "Inspect", risk: "read", category: "data" }}
      rule=""
      saving={false}
      compactPopover
      onSetRule={onSetRule}
    />,
  );
  expect(screen.getByRole("group")).not.toHaveClass("dark-panel-subtle");
  await user.click(screen.getByRole("button", { name: "Prompt" }));
  expect(onSetRule).toHaveBeenLastCalledWith("approval_required");
  view.rerender(<PermissionMutationError value={null} onRetry={vi.fn()} />);
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
});
