import type { ComponentProps, ComponentType, Dispatch, SetStateAction } from "react";
import { ConnectorActionApprovalDialog } from "./connector-action-approval-dialog";
import { ConnectorActivityDialog } from "./connector-activity-dialog";
import { MessagesDialog } from "./messages-dialog";

type Activity = ComponentProps<typeof ConnectorActivityDialog>;
type Approval = ComponentProps<typeof ConnectorActionApprovalDialog>;
type Messages = ComponentProps<typeof MessagesDialog>;

export type ConsoleOperationSlotProps<Value, Completion extends unknown[], Credential = never> = {
  value: Value;
  credentials: Credential[];
  onChange: Dispatch<SetStateAction<Value>>;
  onOperationComplete: (..._args: Completion) => void | Promise<void>;
};

export type ConsolePageDialogsProps<Value, Completion extends unknown[], Credential = never> = {
  activityDialog: {
    open: Activity["open"];
    approvals: Activity["approvals"];
    refresh: Activity["onRefresh"];
    close: Activity["onClose"];
  };
  approvalDialog: {
    activeApproval: Approval["approval"];
    note: Approval["note"];
    action: Approval["action"];
    setNote: Approval["onNoteChange"];
    approve: Approval["onRun"];
    decline: Approval["onDecline"];
    close: Approval["onClose"];
  };
  messageDialog: {
    isOpen: Messages["open"];
    target: Messages["target"];
    tokens: Messages["tokens"];
    tokenID: Messages["tokenID"];
    state: Messages["state"];
    text: Messages["text"];
    setTokenID: Messages["onTokenChange"];
    setText: Messages["onTextChange"];
    submit: Messages["onSubmit"];
    load: Messages["onRefresh"];
    close: Messages["onClose"];
  };
  operationDialog: {
    Template: ComponentType<ConsoleOperationSlotProps<Value, Completion, Credential>> | null;
    value: Value;
    onChange: Dispatch<SetStateAction<Value>>;
    onComplete: ConsoleOperationSlotProps<Value, Completion, Credential>["onOperationComplete"];
  };
};

export function ConsolePageDialogs<Value, Completion extends unknown[], Credential>({
  activityDialog,
  approvalDialog,
  messageDialog,
  operationDialog,
}: ConsolePageDialogsProps<Value, Completion, Credential>) {
  const OperationTemplate = operationDialog.Template;

  return (
    <>
      <ConnectorActionApprovalDialog
        approval={approvalDialog.activeApproval}
        note={approvalDialog.note}
        action={approvalDialog.action}
        onNoteChange={approvalDialog.setNote}
        onRun={approvalDialog.approve}
        onDecline={approvalDialog.decline}
        onClose={approvalDialog.close}
      />
      <ConnectorActivityDialog
        open={activityDialog.open}
        approvals={activityDialog.approvals}
        onRefresh={activityDialog.refresh}
        onClose={activityDialog.close}
      />
      <MessagesDialog
        open={messageDialog.isOpen}
        target={messageDialog.target}
        tokens={messageDialog.tokens}
        tokenID={messageDialog.tokenID}
        state={messageDialog.state}
        text={messageDialog.text}
        onTokenChange={messageDialog.setTokenID}
        onTextChange={messageDialog.setText}
        onSubmit={messageDialog.submit}
        onRefresh={messageDialog.load}
        onClose={messageDialog.close}
      />
      {OperationTemplate ? (
        <OperationTemplate
          value={operationDialog.value}
          credentials={[]}
          onChange={operationDialog.onChange}
          onOperationComplete={operationDialog.onComplete}
        />
      ) : null}
    </>
  );
}
