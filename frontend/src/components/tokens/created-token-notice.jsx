import { X } from "lucide-react";
import { maskedToken } from "../../lib/permissions";
import { Button } from "../ui/button";
import { CopyButton } from "../ui/copy-button";
import { Input } from "../ui/form";
import { Notice } from "../ui/notice";

export function CreatedTokenNotice({ token, onDismiss }) {
  if (!token) return null;
  return (
    <Notice tone="good" className="relative pr-12">
      <Button
        type="button"
        variant="ghost"
        className="absolute right-2 top-2 h-8 w-8 px-0"
        title="Dismiss and clear generated token"
        aria-label="Dismiss generated token"
        onClick={onDismiss}
      >
        <X className="h-4 w-4" />
      </Button>
      <div className="grid gap-2">
        <strong>{token.name} token created.</strong>
        <span className="text-sm">Copy it now. If reusable token copy is off in Settings, this value will not be shown again.</span>
        <div className="flex gap-2">
          <Input className="font-mono text-xs" readOnly value={maskedToken(token.token)} />
          <CopyButton value={token.token} variant="outline" />
        </div>
      </div>
    </Notice>
  );
}
