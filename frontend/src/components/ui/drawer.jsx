import * as DialogPrimitive from "@radix-ui/react-dialog";
import { X } from "lucide-react";
import { Button } from "./button";
import { useDialogFrame } from "./use-dialog-frame";
import { cn } from "../../lib/utils";

export function Drawer({ open, title, description, children, onClose, bodyClassName, className }) {
  const { closeButtonRef, descriptionID, titleID, requestClose, handleOpenAutoFocus, handleCloseAutoFocus } = useDialogFrame({ onClose });

  return (
    <DialogPrimitive.Root
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen) requestClose();
      }}
    >
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-stone-950/30" data-testid="drawer-overlay" />
        <DialogPrimitive.Content
          aria-labelledby={titleID}
          aria-describedby={description ? descriptionID : undefined}
          className={cn(
            "fixed inset-y-0 right-0 z-50 flex w-full max-w-xl flex-col border-l border-stone-200 bg-white shadow-2xl",
            "animate-in slide-in-from-right",
            className,
          )}
          onOpenAutoFocus={handleOpenAutoFocus}
          onCloseAutoFocus={handleCloseAutoFocus}
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
              <Button ref={closeButtonRef} type="button" variant="ghost" className="h-9 w-9 px-0" aria-label="Close drawer">
                <X className="h-4 w-4" />
              </Button>
            </DialogPrimitive.Close>
          </header>
          <div className={cn("min-h-0 min-w-0 flex-1 overflow-auto p-5", bodyClassName)}>{children}</div>
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}
