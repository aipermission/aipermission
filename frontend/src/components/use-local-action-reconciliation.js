import { useEffect, useRef, useState } from "react";
import { localActionReconciliationEvent } from "../lib/local-action-retry";

export function useLocalActionReconciliation() {
  const [value, setValue] = useState(null);
  const resolverRef = useRef(null);

  useEffect(() => {
    function requestReconciliation(event) {
      if (typeof event.detail?.resolve !== "function") return;
      event.preventDefault();
      resolverRef.current?.(false);
      resolverRef.current = event.detail.resolve;
      setValue(event.detail);
    }
    window.addEventListener(localActionReconciliationEvent, requestReconciliation);
    return () => {
      window.removeEventListener(localActionReconciliationEvent, requestReconciliation);
      resolverRef.current?.(false);
      resolverRef.current = null;
    };
  }, []);

  function close(confirmed) {
    resolverRef.current?.(confirmed);
    resolverRef.current = null;
    setValue(null);
  }

  return [value, close];
}
