import assert from "node:assert/strict";
import test from "node:test";
import { reconcileVaultApprovalDialog } from "./vault-approval-poll.js";

const closedDialog = { approval: null, note: "", state: "idle", error: null };

test("dismissed pending Vault approval stays closed until it leaves the pending set", () => {
  const seen = new Set();
  const request = { id: 42, status: "approval_pending" };
  const opened = reconcileVaultApprovalDialog(closedDialog, [request], seen);
  assert.equal(opened.approval.id, 42);

  const afterDismissPoll = reconcileVaultApprovalDialog(closedDialog, [request], seen);
  assert.equal(afterDismissPoll.approval, null);
  assert.deepEqual([...seen], [42]);

  reconcileVaultApprovalDialog(closedDialog, [], seen);
  assert.equal(seen.size, 0);
  const reopenedAfterLeavingPending = reconcileVaultApprovalDialog(closedDialog, [request], seen);
  assert.equal(reopenedAfterLeavingPending.approval.id, 42);
});
