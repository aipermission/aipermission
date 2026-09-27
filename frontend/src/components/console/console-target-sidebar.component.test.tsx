import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  ConsoleTargetSidebar,
  ConsoleStatusDot,
  consoleTargetRows,
  defaultConsoleTargetRef,
  groupConsoleTargetsByProject,
  targetDisplayName,
  targetSubtitle,
  targetProfileLabel,
  targetUsesLiveConsole,
  recoverableRunningActions,
} from "./console-target-sidebar";
import type { ComponentProps } from "react";
import type { GatewayTarget } from "../../lib/gateway-contracts/core-resource-contracts";

const admin = {
  connector_kind: "postgres",
  target_id: 7,
  profile_id: 11,
  ref: "postgres:7:11",
  project_id: 3,
  project_name: "My Project",
};
const readonly = { ...admin, profile_id: 12, ref: "postgres:7:12" };
const cache = {
  connector_kind: "redis",
  target_id: 8,
  profile_id: 13,
  ref: "redis:8:13",
  project_id: null,
  project_name: "",
  runtime_id: 9,
};

describe("console target navigation", () => {
  it("keeps one row per connector and preserves the preferred profile", () => {
    const rows = consoleTargetRows([admin, readonly, cache], null, { "postgres:7": 12 });

    expect(rows).toHaveLength(2);
    expect(rows[0].ref).toBe("postgres:7:12");
    expect(rows[1].ref).toBe("redis:8:13");
  });

  it("groups unassigned connectors and prioritizes pending or unread targets", () => {
    const groups = groupConsoleTargetsByProject([admin, cache]);
    expect(groups.map((group) => group.name)).toEqual(["My Project", "Ungrouped"]);
    expect(defaultConsoleTargetRef([admin, cache], [], [{ target_ref: cache.ref }])).toBe(cache.ref);
    expect(defaultConsoleTargetRef([admin, cache], [{ runtime_id: 9 }], [])).toBe(cache.ref);
  });
});

const target: GatewayTarget = {
  ...admin,
  target_name: "Database",
  profile_kind: "fixture",
  profile_label: "Admin",
  project_slug: "my-project",
  status: "active",
  created_at: "2026-09-26",
  updated_at: "2026-09-26",
};

it("keeps malformed native targets navigable without enabling live console capabilities", () => {
  const malformed = { ...target, connector_kind: "mail", config: { imap_host: 42 } };
  expect(targetDisplayName(malformed)).toBe("Database");
  expect(targetSubtitle(malformed)).toBe("mail profile Admin");
  expect(targetProfileLabel(malformed)).toBe("Admin");
  expect(targetUsesLiveConsole(malformed)).toBe(false);
  expect(recoverableRunningActions(malformed)).toEqual([]);
  renderSidebar({
    targetRows: [malformed],
    targetItems: [malformed],
    selectedTarget: malformed,
    groups: groupConsoleTargetsByProject([malformed]),
  });
  expect(screen.getByText("Database")).toBeVisible();
});

function renderSidebar(overrides: Partial<ComponentProps<typeof ConsoleTargetSidebar>> = {}) {
  const onSelect = vi.fn();
  const onCompactChange = vi.fn();
  render(
    <ConsoleTargetSidebar
      compact={false}
      onCompactChange={onCompactChange}
      targetRows={[target]}
      search=""
      onSearch={vi.fn()}
      groups={groupConsoleTargetsByProject([target])}
      collapsedProjects={{}}
      onToggleProject={vi.fn()}
      targetItems={[target]}
      liveConsoleTargets={{ data: [] }}
      sessions={[]}
      selectedTarget={target}
      pendingConnectorApprovals={[]}
      connectorActionApprovals={{ data: [] }}
      unreadMessages={[]}
      onSelect={onSelect}
      targetsState="ready"
      targetsError={null}
      filteredTargetCount={1}
      {...overrides}
    />,
  );
  return { onSelect, onCompactChange };
}

it("renders a selectable connector with its profile and accessible status", async () => {
  const user = userEvent.setup();
  const { onSelect } = renderSidebar();
  expect(screen.getByText("Database")).toBeVisible();
  expect(screen.getByText("Admin")).toBeVisible();
  expect(screen.getByLabelText("Target ready")).toBeInTheDocument();
  await user.click(screen.getByText("Database"));
  expect(onSelect).toHaveBeenCalledWith(target);
});

it("preserves compact navigation without the search field", async () => {
  const user = userEvent.setup();
  const { onCompactChange } = renderSidebar({ compact: true });
  expect(screen.queryByPlaceholderText("Search connectors")).not.toBeInTheDocument();
  await user.click(screen.getByTitle("Expand connectors"));
  expect(onCompactChange).toHaveBeenCalledWith(false);
});

it.each(["offline", "idle", "busy"] as const)("describes the %s status without relying on color", (status) => {
  render(<ConsoleStatusDot status={status} />);
  const text = { offline: "No live session", idle: "Target ready", busy: "Pending or running work" }[status];
  expect(screen.getByLabelText(text)).toBeInTheDocument();
});
