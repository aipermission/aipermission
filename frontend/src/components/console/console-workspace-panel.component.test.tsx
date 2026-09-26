import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { ConsoleWorkspacePanel } from "./console-workspace-panel";
import type { ConsoleToolbarSlotProps, ConsoleWorkspacePanelProps, ConsoleWorkspaceSlotProps } from "./console-workspace-types";
import { gatewayTargetFixture } from "../../test/connector-inventory-fixtures";
import { connectorApprovalFixture } from "../../test/connector-action-fixtures";

vi.mock("./pty-console", () => ({ PtyConsole: () => <div data-testid="pty-console" /> }));

function panelProps(overrides: Partial<ConsoleWorkspacePanelProps> = {}): ConsoleWorkspacePanelProps {
  const selectedTarget = gatewayTargetFixture({ ref: "example:1:1", name: "main-db", target_name: "main-db", profile_id: 1 });
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
      Console: ({ children, session }: ConsoleWorkspaceSlotProps) => <div data-testid="connector-console">{session && "active" in session && session.active ? "active" : children}</div>,
      ToolbarActions: null,
    },
    liveConsoleTargets: [],
    sessionView: {
      selectedSession: { id: 0, status: "idle" },
      selectedSessionLive: false,
      selectedStructuredSession: { active: true, startedAt: "2026-09-26" },
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
    const pending = connectorApprovalFixture({ id: 7 });
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
    const target = value.targetView.selectedTarget;
    if (!target) throw new Error("Expected a selected target fixture");
    value.targetView.selectedTargetProfiles = [
      target,
      { ...target, profile_id: 2, profile_label: "Read only" },
    ];
    render(<ConsoleWorkspacePanel {...value} />);

    await user.selectOptions(screen.getByLabelText("Profile"), "2");

    expect(value.actions.selectProfile).toHaveBeenCalledWith("2");
  });

  it("mounts the live terminal only for an active live-console session", () => {
    const value = panelProps();
    value.targetView.selectedRuntimeTarget = { id: 41, name: "Live target" };
    value.sessionView.targetUsesLiveConsole = true;
    value.sessionView.selectedSessionLive = true;
    value.sessionView.selectedSession = { id: 7, status: "connected", transcript: "Ready" };
    value.sessionView.selectedStructuredSession = null;
    render(<ConsoleWorkspacePanel {...value} />);

    expect(screen.getByTestId("pty-console")).toBeInTheDocument();
    expect(screen.queryByText("No active shell session")).not.toBeInTheDocument();
  });

  it("shows loading before offering a new session and forwards session creation", () => {
    const value = panelProps();
    value.targetView.selectedRuntimeTarget = { id: 41, name: "Live target" };
    value.sessionView.targetUsesLiveConsole = true;
    value.sessionView.selectedStructuredSession = null;
    value.sessionView.sessionsState = "loading";
    const view = render(<ConsoleWorkspacePanel {...value} />);

    expect(screen.getByText("Loading console sessions...")).toBeInTheDocument();
    expect(screen.queryByText("No active shell session")).not.toBeInTheDocument();
    value.sessionView.sessionsState = "ready";
    view.rerender(<ConsoleWorkspacePanel {...value} />);
    expect(screen.getByText("No active shell session")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "New Session" }));
    expect(value.actions.startLiveSession).toHaveBeenCalledOnce();
  });

  it("does not attach a live terminal when its runtime target is unavailable", () => {
    const value = panelProps();
    value.sessionView.targetUsesLiveConsole = true;
    value.sessionView.selectedStructuredSession = null;
    render(<ConsoleWorkspacePanel {...value} />);

    expect(screen.getByText("Select a live-console connector.")).toBeInTheDocument();
    expect(screen.queryByTestId("pty-console")).not.toBeInTheDocument();
  });

  it("preserves a closed session's timestamp in the empty-session guidance", () => {
    const value = panelProps();
    value.targetView.selectedRuntimeTarget = { id: 41, name: "Live target" };
    value.sessionView.targetUsesLiveConsole = true;
    value.sessionView.selectedStructuredSession = null;
    value.sessionView.selectedSession = { id: 7, status: "closed", closed_at: "2026-09-26T12:00:00Z" };
    render(<ConsoleWorkspacePanel {...value} />);

    expect(screen.getByText(/last Live target session is closed/i)).toBeInTheDocument();
    expect(screen.getByText(/Last session:/)).toBeInTheDocument();
  });

  it("renders missing-template guidance instead of an unrelated connector console", () => {
    const value = panelProps();
    value.connectorView.Console = null;
    render(<ConsoleWorkspacePanel {...value} />);

    expect(screen.getByText(/Connector template not found: example\/console/)).toBeInTheDocument();
    expect(screen.queryByTestId("connector-console")).not.toBeInTheDocument();
  });

  it("passes workspace-owned session controls to connector-owned toolbar slots", () => {
    const value = panelProps();
    value.connectorView.ToolbarActions = ({ structuredSession, onNewStructuredSession, onEndStructuredSession }: ConsoleToolbarSlotProps) => (
      <div>
        <span>{structuredSession?.active ? "Session active" : "No structured session"}</span>
        <button onClick={onNewStructuredSession}>Start fixture session</button>
        <button onClick={onEndStructuredSession}>End fixture session</button>
      </div>
    );
    render(<ConsoleWorkspacePanel {...value} />);

    expect(screen.getByText("Session active")).toBeInTheDocument();
    fireEvent.click(screen.getByText("Start fixture session"));
    fireEvent.click(screen.getByText("End fixture session"));
    expect(value.actions.startStructuredSession).toHaveBeenCalledOnce();
    expect(value.actions.endStructuredSession).toHaveBeenCalledOnce();
  });

  it("keeps simultaneous safety and recovery notices visible without replacing the console", () => {
    const value = panelProps();
    value.warnings = {
      ...value.warnings, bannerCount: 3, showAlwaysRun: true, alwaysRunTokenCount: 2,
      temporaryAlwaysRunLabels: ["Temporary fixture grant"],
      runningRequest: { created_at: new Date(value.warnings.now - 30000).toISOString(), action_name: "inspect", input: {} },
      newSessionError: "Fixture session failed",
    };
    render(<ConsoleWorkspacePanel {...value} />);

    expect(screen.getByText(/2 tokens can run connector actions/)).toBeInTheDocument();
    expect(screen.getByText(/Temporary fixture grant/)).toBeInTheDocument();
    expect(screen.getByText("Connector action running")).toBeInTheDocument();
    expect(screen.getByText("Fixture session failed")).toBeInTheDocument();
    expect(screen.getByTestId("connector-console")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Restart" }));
    expect(value.actions.restartSession).toHaveBeenCalledOnce();
  });
});
