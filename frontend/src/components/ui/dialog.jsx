import { useEffect, useRef } from "react";
import { X } from "lucide-react";
import { Button } from "./button";
import { cn } from "../../lib/utils";

const sizes = {
  sm: "max-w-md",
  md: "max-w-lg",
  lg: "max-w-2xl",
  xl: "max-w-4xl",
  wide: "max-w-[calc(100vw-80px)]",
};

export function Dialog({ open, title, description, children, onClose, size = "sm", className = "", bodyClassName = "", autoFocusClose = true }) {
  const closeButtonRef = useRef(null);
  const onCloseRef = useRef(onClose);

  useEffect(() => {
    onCloseRef.current = onClose;
  }, [onClose]);

  useEffect(() => {
    if (!open) return undefined;
    const previous = document.activeElement;
    if (autoFocusClose) {
      closeButtonRef.current?.focus();
    }
    const onKeyDown = (event) => {
      if (event.key === "Escape") {
        onCloseRef.current?.();
      }
    };
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("keydown", onKeyDown);
      previous?.focus?.();
    };
  }, [open]);

  if (!open) return null;

  return (
    <div className="fixed inset-0 z-50 grid place-items-center p-4">
      <button type="button" className="dialog-overlay absolute inset-0 bg-stone-950/45" aria-label="Close dialog" onClick={onClose} />
      <section role="dialog" aria-modal="true" aria-labelledby="dialog-title" className={`relative grid w-full ${sizes[size] || sizes.sm} overflow-hidden rounded-lg border border-stone-200 bg-white shadow-2xl ${className}`}>
        <header className="flex items-start justify-between gap-4 border-b border-stone-200 p-5">
          <div>
            <h2 id="dialog-title" className="text-lg font-semibold text-stone-950">{title}</h2>
            {description ? <p className="mt-1 text-sm text-stone-500">{description}</p> : null}
          </div>
          <Button ref={closeButtonRef} type="button" variant="ghost" className="h-9 w-9 px-0" onClick={onClose}>
            <X className="h-4 w-4" />
          </Button>
        </header>
        <div className={cn("p-5", bodyClassName)}>{children}</div>
      </section>
    </div>
  );
}
