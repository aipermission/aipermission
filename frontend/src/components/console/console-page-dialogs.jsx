import { ConnectorActionApprovalDialog } from "./connector-action-approval-dialog";
import { ConnectorActivityDialog } from "./connector-activity-dialog";
import { MessagesDialog } from "./messages-dialog";

export function ConsolePageDialogs({ activityDialog, approvalDialog, messageDialog, operationDialog }) {
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
