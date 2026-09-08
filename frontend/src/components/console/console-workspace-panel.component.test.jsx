import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { ConsoleWorkspacePanel } from "./console-workspace-panel";

vi.mock("./pty-console", () => ({ PtyConsole: () => <div data-testid="pty-console" /> }));

function panelProps(overrides = {}) {
  const selectedTarget = { ref: "postgres:1:1", connector_kind: "postgres", name: "main-db", profile_id: 1 };
  return {
    actions: {
      endLiveSession: vi.fn(),
      endStructuredSession: vi.fn(),
      interruptSession: vi.fn(),
      openActivity: vi.fn(),
      openApproval: vi.fn(),
      openMessages: vi.fn(),
      refreshActivity: vi.fn(),
      refreshSessions: vi.fn(),
      resizeSession: vi.fn(),
      restartSession: vi.fn(),
      selectLiveSessionName: vi.fn(),
      selectProfile: vi.fn(),
      sendInput: vi.fn(),
      startLiveSession: vi.fn(),
      startLiveSessionWithOptions: vi.fn(),
      startStructuredSession: vi.fn(),
    },
    approvals: { state: "ready", data: [], error: null },
    connectorView: {
      Console: ({ children, session }) => <div data-testid="connector-console">{session?.active ? "active" : children}</div>,
      ToolbarActions: null,
    },
    liveConsoleTargets: [],
    sessionView: {
      selectedSession: { id: 0, status: "idle" },
      selectedSessionLive: false,
      selectedStructuredSession: { active: true },
      sessionsState: "ready",
      targetUsesLiveConsole: false,
    },
    targetView: {
      runningApprovalCount: 0,
      selectedPendingApprovals: [],
      selectedRuntimeTarget: null,
      selectedTarget,
      selectedTargetProfiles: [selectedTarget],
      selectedUnreadMessages: [],
    },
    theme: "dark",
    warnings: {
      alwaysRunTokenCount: 0,
      bannerCount: 0,
      newSessionError: "",
      now: Date.now(),
      restartAction: { state: "idle", error: null },
      runningRequest: null,
      showAlwaysRun: false,
      temporaryAlwaysRunLabels: [],
    },
    ...overrides,
  };
}

describe("ConsoleWorkspacePanel", () => {
  it("renders the selected connector template without connector-kind branching", () => {
    render(<ConsoleWorkspacePanel {...panelProps()} />);

    expect(screen.getByRole("heading", { level: 2, name: "main-db" })).toBeInTheDocument();
    expect(screen.getByTestId("connector-console")).toHaveTextContent("active");
  });

  it("forwards the selected pending approval from the header", () => {
    const value = panelProps();
    const pending = { id: 7, status: "approval_pending" };
    value.targetView.selectedPendingApprovals = [pending];
    render(<ConsoleWorkspacePanel {...value} />);

    fireEvent.click(screen.getByTitle("Pending connector approvals for this target"));

    expect(value.actions.openApproval).toHaveBeenCalledWith(pending);
  });

  it("shows the empty target state without mounting a connector template", () => {
    const value = panelProps();
    value.targetView.selectedTarget = null;
    value.targetView.selectedTargetProfiles = [];
    render(<ConsoleWorkspacePanel {...value} />);

    expect(screen.getByText("Select a target.")).toBeInTheDocument();
    expect(screen.queryByTestId("connector-console")).not.toBeInTheDocument();
  });

  it("keeps profile selection available in the workspace header", async () => {
    const user = userEvent.setup();
    const value = panelProps();
    value.targetView.selectedTargetProfiles = [
      value.targetView.selectedTarget,
      { ...value.targetView.selectedTarget, profile_id: 2, profile_label: "Read only" },
    ];
    render(<ConsoleWorkspacePanel {...value} />);

    await user.selectOptions(screen.getByLabelText("Profile"), "2");

    expect(value.actions.selectProfile).toHaveBeenCalledWith("2");
  });
});
