import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ConsolePageDialogs } from "./console-page-dialogs";
import type { ComponentProps } from "react";
import type { ConsoleOperationSlotProps, ConsolePageDialogsProps } from "./console-page-dialogs";
import type { ConnectorActionApprovalDialog } from "./connector-action-approval-dialog";
import type { ConnectorActivityDialog } from "./connector-activity-dialog";
import type { MessagesDialog } from "./messages-dialog";

type Value = { open: boolean };
type Completion = [{ ok: boolean }];

vi.mock("./connector-action-approval-dialog", () => ({
  ConnectorActionApprovalDialog: ({ onRun }: Pick<ComponentProps<typeof ConnectorActionApprovalDialog>, "onRun">) => (
    <button onClick={onRun}>Approve test</button>
  ),
}));
vi.mock("./connector-activity-dialog", () => ({
  ConnectorActivityDialog: ({ onClose }: Pick<ComponentProps<typeof ConnectorActivityDialog>, "onClose">) => (
    <button onClick={onClose}>Close activity test</button>
  ),
}));
vi.mock("./messages-dialog", () => ({
  MessagesDialog: ({ onSubmit }: Pick<ComponentProps<typeof MessagesDialog>, "onSubmit">) => (
    <button onClick={onSubmit}>Send message test</button>
  ),
}));

function dialogProps() {
  const value: Value = { open: true };
  return {
    activityDialog: {
      approvals: { state: "ready", data: [], error: null },
      close: vi.fn(),
      open: true,
      refresh: vi.fn(),
    },
    approvalDialog: {
      action: { state: "idle", error: null },
      activeApproval: null,
      approve: vi.fn(async () => {}),
      close: vi.fn(),
      decline: vi.fn(async () => {}),
      note: "",
      setNote: vi.fn(),
    },
    messageDialog: {
      close: vi.fn(),
      isOpen: true,
      load: vi.fn(),
      setText: vi.fn(),
      setTokenID: vi.fn(),
      state: { state: "idle", data: [], error: null },
      submit: vi.fn(async () => {}),
      target: { name: "Example" },
      text: "hello",
      tokenID: "3",
      tokens: [{ id: 3, name: "Example" }],
    },
    operationDialog: {
      onChange: vi.fn(),
      onComplete: vi.fn(),
      Template: ({ onOperationComplete }: ConsoleOperationSlotProps<Value, Completion>) => (
        <button onClick={() => onOperationComplete({ ok: true })}>Complete operation test</button>
      ),
      value,
    },
  } satisfies ConsolePageDialogsProps<Value, Completion>;
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
    render(<ConsolePageDialogs {...props} operationDialog={{ ...props.operationDialog, Template: null }} />);

    expect(screen.queryByText("Complete operation test")).not.toBeInTheDocument();
  });

  it("preserves connector-owned completion arguments and functional state updates", () => {
    const value = { open: true, operationID: 9 };
    type Result = { message: string };
    type NativeCompletion = [Result, typeof value];
    const onChange = vi.fn<React.Dispatch<React.SetStateAction<typeof value>>>();
    const onComplete = vi.fn<(..._args: NativeCompletion) => void>();
    const Template = ({ onChange, onOperationComplete }: ConsoleOperationSlotProps<typeof value, NativeCompletion>) => (
      <button
        onClick={() => {
          onChange((current) => ({ ...current, open: false }));
          void onOperationComplete({ message: "Saved" }, value);
        }}
      >
        Complete native operation
      </button>
    );
    render(<ConsolePageDialogs {...dialogProps()} operationDialog={{ Template, value, onChange, onComplete }} />);

    fireEvent.click(screen.getByText("Complete native operation"));

    expect(onComplete).toHaveBeenCalledWith({ message: "Saved" }, value);
    const update: React.SetStateAction<typeof value> = onChange.mock.calls[0][0];
    expect(typeof update).toBe("function");
    if (typeof update !== "function") throw new Error("Expected a functional operation update");
    expect(update(value)).toEqual({ open: false, operationID: 9 });
  });
});
