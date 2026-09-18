import { ShieldCheck } from "lucide-react";
import { Button } from "../ui/button";
import { Notice } from "../ui/notice";

export function PendingVaultApprovalNotice({ count, onReview }) {
  return (
    <Notice tone="warn" className="flex items-center justify-between gap-3">
      <span>
        {count} pending Vault approval{count === 1 ? "" : "s"}
      </span>
      <Button type="button" variant="outline" aria-label="Review pending Vault approval" onClick={onReview}>
        <ShieldCheck size={16} aria-hidden="true" /> Review
      </Button>
    </Notice>
  );
}
