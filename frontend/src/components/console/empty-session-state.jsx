import { RefreshCcw, TerminalSquare } from "lucide-react";
import { Button } from "../ui/button";

export function EmptySessionState({ title, description, detail, onStart, theme = "dark", buttonLabel = "New Session" }) {
  const light = theme === "light";
  return (
    <div className={`grid h-full min-h-0 place-items-center p-6 ${light ? "text-stone-700" : "text-stone-200"}`}>
      <div className="grid max-w-md gap-4 text-center">
        <div
          className={`mx-auto flex h-12 w-12 items-center justify-center rounded-full border ${light ? "border-stone-200 bg-stone-100" : "border-stone-600 bg-stone-800"}`}
        >
          <TerminalSquare className={`h-6 w-6 ${light ? "text-stone-600" : "text-stone-300"}`} aria-hidden="true" />
        </div>
        <div className="grid gap-2">
          <h3 className={`text-base font-semibold ${light ? "text-stone-950" : "text-white"}`}>{title}</h3>
          <p className={`text-sm leading-6 ${light ? "text-stone-600" : "text-stone-400"}`}>{description}</p>
          {detail ? <p className="text-xs text-stone-500">{detail}</p> : null}
        </div>
        <Button type="button" className="mx-auto" onClick={onStart}>
          <RefreshCcw className="h-4 w-4" />
          {buttonLabel}
        </Button>
      </div>
    </div>
  );
}
