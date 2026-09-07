import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ConsolePageDialogs } from "./console-page-dialogs";

vi.mock("./connector-action-approval-dialog", () => ({
  ConnectorActionApprovalDialog: ({ onRun }) => <button onClick={onRun}>Approve test</button>,
}));
vi.mock("./connector-activity-dialog", () => ({
  ConnectorActivityDialog: ({ onClose }) => <button onClick={onClose}>Close activity test</button>,
}));
vi.mock("./messages-dialog", () => ({
  MessagesDialog: ({ onSubmit }) => <button onClick={onSubmit}>Send message test</button>,
}));

function dialogProps() {
  return {
    activityDialog: {
      approvals: { data: [] },
      close: vi.fn(),
      open: true,
      refresh: vi.fn(),
    },
    approvalDialog: {
      action: { state: "idle" },
      activeApproval: { id: 1 },
      approve: vi.fn(),
      close: vi.fn(),
      decline: vi.fn(),
      note: "",
      setNote: vi.fn(),
    },
    messageDialog: {
      close: vi.fn(),
      isOpen: true,
      load: vi.fn(),
      setText: vi.fn(),
      setTokenID: vi.fn(),
      state: { state: "idle" },
      submit: vi.fn(),
      target: { id: 2 },
      text: "hello",
      tokenID: 3,
      tokens: [{ id: 3 }],
    },
    operationDialog: {
      onChange: vi.fn(),
      onComplete: vi.fn(),
      Template: ({ onOperationComplete }) => <button onClick={() => onOperationComplete({ ok: true })}>Complete operation test</button>,
      value: { open: true },
    },
  };
}

describe("ConsolePageDialogs", () => {
  it("forwards each shared dialog action to its behavior owner", () => {
    const props = dialogProps();
    render(<ConsolePageDialogs {...props} />);

    fireEvent.click(screen.getByText("Approve test"));
    fireEvent.click(screen.getByText("Close activity test"));
    fireEvent.click(screen.getByText("Send message test"));
    fireEvent.click(screen.getByText("Complete operation test"));

    expect(props.approvalDialog.approve).toHaveBeenCalledOnce();
    expect(props.activityDialog.close).toHaveBeenCalledOnce();
    expect(props.messageDialog.submit).toHaveBeenCalledOnce();
    expect(props.operationDialog.onComplete).toHaveBeenCalledWith({ ok: true });
  });

  it("does not mount a connector operation when its template is unavailable", () => {
    const props = dialogProps();
    props.operationDialog.Template = null;
    render(<ConsolePageDialogs {...props} />);

    expect(screen.queryByText("Complete operation test")).not.toBeInTheDocument();
  });
});
