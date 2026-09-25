import { useEffect, useId, useRef } from "react";

export function useDialogFrame({
  onClose,
  autoFocusClose = true,
  closeDisabled = false,
}: {
  onClose?: () => void;
  autoFocusClose?: boolean;
  closeDisabled?: boolean;
}) {
  const closeButtonRef = useRef<HTMLButtonElement | null>(null);
  const onCloseRef = useRef(onClose);
  const restoreFocusRef = useRef<Element | null>(null);
  const titleID = useId();
  const descriptionID = useId();

  useEffect(() => {
    onCloseRef.current = onClose;
  }, [onClose]);

  return {
    closeButtonRef,
    descriptionID,
    titleID,
    requestClose() {
      if (!closeDisabled) onCloseRef.current?.();
    },
    handleOpenAutoFocus(event: Event) {
      restoreFocusRef.current = document.activeElement;
      if (!autoFocusClose || closeDisabled) return;
      event.preventDefault();
      closeButtonRef.current?.focus();
    },
    handleCloseAutoFocus(event: Event) {
      event.preventDefault();
      const previousFocus = restoreFocusRef.current;
      if (previousFocus && "focus" in previousFocus && typeof previousFocus.focus === "function") previousFocus.focus();
      restoreFocusRef.current = null;
    },
  };
}
