import { X } from "lucide-react";
import { Button } from "./button";
import { cn } from "../../lib/utils";

export function Drawer({ open, title, description, children, onClose, bodyClassName }) {
  if (!open) return null;

  return (
    <div className="fixed inset-0 z-50">
      <button type="button" className="absolute inset-0 bg-stone-950/30" aria-label="Close drawer" onClick={onClose} />
      <aside
        className={cn(
          "absolute inset-y-0 right-0 flex w-full max-w-xl flex-col border-l border-stone-200 bg-white shadow-2xl",
          "animate-in slide-in-from-right",
        )}
      >
        <header className="flex items-start justify-between gap-4 border-b border-stone-200 p-5">
          <div>
            <h2 className="text-lg font-semibold text-stone-950">{title}</h2>
            {description ? <p className="mt-1 text-sm text-stone-500">{description}</p> : null}
          </div>
          <Button type="button" variant="ghost" className="h-9 w-9 px-0" onClick={onClose}>
            <X className="h-4 w-4" />
          </Button>
        </header>
        <div className={cn("min-h-0 min-w-0 flex-1 overflow-auto p-5", bodyClassName)}>{children}</div>
      </aside>
    </div>
  );
}
