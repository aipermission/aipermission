import { useCallback, useEffect, useRef, useState } from "react";

export const idleActionState = { state: "idle", error: null, message: null };

export function useAsyncAction(initialState = idleActionState) {
  const [actionState, setActionState] = useState(initialState);
  const generationRef = useRef(0);
  const mountedRef = useRef(false);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      generationRef.current += 1;
    };
  }, []);

  const runAction = useCallback(async ({ pending = "saving", successMessage = null, action, onError }) => {
    const generation = ++generationRef.current;
    setActionState({ state: pending, error: null, message: null });
    try {
      const result = await action();
      if (!mountedRef.current || generation !== generationRef.current) return result;
      const message = typeof successMessage === "function" ? successMessage(result) : successMessage;
      setActionState({ state: "idle", error: null, message });
      return result;
    } catch (error) {
      if (!mountedRef.current || generation !== generationRef.current) return undefined;
      if (onError?.(error)) {
        setActionState(idleActionState);
        return undefined;
      }
      setActionState({ state: "error", error: error.message, message: null });
      return undefined;
    }
  }, []);

  const resetAction = useCallback(() => {
    generationRef.current += 1;
    setActionState(idleActionState);
  }, []);

  return { actionState, setActionState, runAction, resetAction };
}
