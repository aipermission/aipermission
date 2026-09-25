type ApprovalItem = { id: number };
type ApprovalDialog<Item extends ApprovalItem> = { approval: Item | null; note: string; state: string; error: string | null };

export function reconcileVaultApprovalDialog<Item extends ApprovalItem>(
  current: ApprovalDialog<Item>,
  pending: Item[],
  seenRequestIDs: Set<number>,
): ApprovalDialog<Item> {
  const pendingIDs = new Set(pending.map((item) => item.id));
  for (const requestID of seenRequestIDs) {
    if (!pendingIDs.has(requestID) && current.approval?.id !== requestID) {
      seenRequestIDs.delete(requestID);
    }
  }
  if (current.approval) {
    if (pendingIDs.has(current.approval.id) || !["idle", "error"].includes(current.state)) return current;
    return {
      ...current,
      state: "stale",
      error: "This Vault approval is no longer pending. It may have expired, been canceled, or changed in another client.",
    };
  }
  const next = pending.find((item) => !seenRequestIDs.has(item.id));
  if (!next) return current;
  seenRequestIDs.add(next.id);
  return { approval: next, note: "", state: "idle", error: null };
}
