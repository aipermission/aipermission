import { Copy, TerminalSquare } from "lucide-react";
import { Button } from "../../../components/ui/button";
import { Notice } from "../../../components/ui/notice";

type Props = {
  command: string;
  reason: string;
  confirmation: string;
  confirmationText: string;
  parallelism: number;
  canRun: boolean;
  error: string | null;
  activityError?: string;
  ownershipNotice: string;
  setCommand: (_value: string) => void;
  setReason: (_value: string) => void;
  setConfirmation: (_value: string) => void;
};

export function BulkCommandInputs(props: Props) {
  return (
    <div className="grid gap-3">
      <label className="grid gap-1 text-sm font-semibold text-stone-700">
        Command
        <textarea
          className="h-32 resize-none rounded-md border border-stone-300 px-3 py-2 font-mono text-sm font-normal outline-none focus:border-emerald-700"
          value={props.command}
          onChange={(event) => props.setCommand(event.target.value)}
          placeholder="apt update"
        />
      </label>
      <label className="grid gap-1 text-sm font-semibold text-stone-700">
        Reason
        <input
          className="h-10 rounded-md border border-stone-300 px-3 text-sm font-normal outline-none focus:border-emerald-700"
          value={props.reason}
          onChange={(event) => props.setReason(event.target.value)}
          placeholder="optional"
        />
      </label>
      <Notice tone="warn" className="flex h-10 items-center overflow-hidden px-3 py-0 text-xs">
        <span className="min-w-0 truncate">
          Type <span className="font-mono font-semibold">{props.confirmationText}</span>
          <button
            type="button"
            className="ml-1 inline-grid h-5 w-5 place-items-center align-[-3px] text-amber-900 hover:text-amber-700"
            title="Copy confirmation phrase"
            onClick={() => copyConfirmationText(props.confirmationText)}
          >
            <Copy className="h-3.5 w-3.5" />
          </button>{" "}
          before starting. Runs {props.parallelism} targets at a time.
        </span>
      </Notice>
      <div className="grid gap-2 sm:grid-cols-[minmax(0,1fr)_auto]">
        <input
          className="h-10 rounded-md border border-stone-300 px-3 font-mono text-sm outline-none focus:border-emerald-700"
          value={props.confirmation}
          onChange={(event) => props.setConfirmation(event.target.value)}
          placeholder={props.confirmationText}
        />
        <Button type="submit" disabled={!props.canRun}>
          <TerminalSquare className="h-4 w-4" />
          Run selected
        </Button>
      </div>
      {props.error ? <Notice tone="bad">{props.error}</Notice> : null}
      {props.activityError ? <Notice tone="warn">Activity refresh: {props.activityError}</Notice> : null}
      {props.ownershipNotice ? <Notice tone="warn">{props.ownershipNotice}</Notice> : null}
    </div>
  );
}

function copyConfirmationText(value: string) {
  if (typeof navigator === "undefined" || !navigator.clipboard) return;
  void navigator.clipboard.writeText(value);
}
