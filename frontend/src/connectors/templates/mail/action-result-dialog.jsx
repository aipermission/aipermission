import { CopyButton } from "../../../components/ui/copy-button";
import { Dialog } from "../../../components/ui/dialog";
import { TerminalBlock } from "../../../components/ui/terminal-block";

export function MailActionResultDialog({ value, onClose }) {
  const rawValue = JSON.stringify({
    status: value?.item?.status || "completed",
    action_name: value?.actionName || "",
    output: value?.item?.output ?? null,
  }, null, 2);

  return (
    <Dialog open={Boolean(value?.open)} title="Mail action result" description={value?.summary || "Completed Mail action output."} onClose={onClose} size="lg" closeOnOverlay={false}>
      <div className="grid gap-3">
        <div className="flex justify-end">
          <CopyButton value={rawValue} variant="outline" className="h-8 px-2 text-xs" />
        </div>
        <TerminalBlock surface="log" className="max-h-[520px] text-xs">{rawValue}</TerminalBlock>
      </div>
    </Dialog>
  );
}
