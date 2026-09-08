import { useEffect, useId, useRef } from "react";

export function useDialogFrame({ onClose, autoFocusClose = true, closeDisabled = false }) {
  const closeButtonRef = useRef(null);
  const onCloseRef = useRef(onClose);
  const restoreFocusRef = useRef(null);
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
    handleOpenAutoFocus(event) {
      restoreFocusRef.current = document.activeElement;
      if (!autoFocusClose || closeDisabled) return;
      event.preventDefault();
      closeButtonRef.current?.focus();
    },
    handleCloseAutoFocus(event) {
      event.preventDefault();
      restoreFocusRef.current?.focus?.();
      restoreFocusRef.current = null;
    },
  };
}
