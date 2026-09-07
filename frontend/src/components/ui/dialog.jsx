import * as DialogPrimitive from "@radix-ui/react-dialog";
import { X } from "lucide-react";
import { useEffect, useId, useRef } from "react";
import { Button } from "./button";
import { cn } from "../../lib/utils";

const sizes = {
  sm: "max-w-md",
  md: "max-w-lg",
  lg: "max-w-2xl",
  xl: "max-w-4xl",
  wide: "max-w-[calc(100vw-80px)]",
};

export function Dialog({
  open,
  title,
  description,
  children,
  onClose,
  size = "sm",
  className = "",
  bodyClassName = "",
  autoFocusClose = true,
  closeOnOverlay = true,
  closeOnEscape = true,
  closeDisabled = false,
}) {
  const closeButtonRef = useRef(null);
  const onCloseRef = useRef(onClose);
  const restoreFocusRef = useRef(null);
  const titleID = useId();
  const descriptionID = useId();

  useEffect(() => {
    onCloseRef.current = onClose;
  }, [onClose]);

  return (
    <DialogPrimitive.Root
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen && !closeDisabled) onCloseRef.current?.();
      }}
    >
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="dialog-overlay fixed inset-0 z-50 bg-stone-950/45" data-testid="dialog-overlay" />
        <div className="pointer-events-none fixed inset-0 z-50 grid place-items-center p-4">
          <DialogPrimitive.Content
            aria-labelledby={titleID}
            aria-describedby={description ? descriptionID : undefined}
            className={`pointer-events-auto relative grid w-full ${sizes[size] || sizes.sm} overflow-hidden rounded-lg border border-stone-200 bg-white shadow-2xl ${className}`}
            onOpenAutoFocus={(event) => {
              restoreFocusRef.current = document.activeElement;
              if (!autoFocusClose || closeDisabled) return;
              event.preventDefault();
              closeButtonRef.current?.focus();
            }}
            onCloseAutoFocus={(event) => {
              event.preventDefault();
              restoreFocusRef.current?.focus?.();
              restoreFocusRef.current = null;
            }}
            onEscapeKeyDown={(event) => {
              if (closeDisabled || !closeOnEscape) event.preventDefault();
            }}
            onPointerDownOutside={(event) => {
              if (closeDisabled || !closeOnOverlay) event.preventDefault();
            }}
          >
            <header className="flex items-start justify-between gap-4 border-b border-stone-200 p-5">
              <div>
                <DialogPrimitive.Title id={titleID} className="text-lg font-semibold text-stone-950">
                  {title}
                </DialogPrimitive.Title>
                {description ? (
                  <DialogPrimitive.Description id={descriptionID} className="mt-1 text-sm text-stone-500">
                    {description}
                  </DialogPrimitive.Description>
                ) : null}
              </div>
              <DialogPrimitive.Close asChild>
                <Button
                  ref={closeButtonRef}
                  type="button"
                  variant="ghost"
                  className="h-9 w-9 px-0"
                  aria-label="Close dialog"
                  disabled={closeDisabled}
                >
                  <X className="h-4 w-4" />
                </Button>
              </DialogPrimitive.Close>
            </header>
            <div className={cn("p-5", bodyClassName)}>{children}</div>
          </DialogPrimitive.Content>
        </div>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}
