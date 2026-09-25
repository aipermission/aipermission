import { useCallback, useEffect, useRef, useState } from "react";
import { errorMessage } from "./errors";

export type AsyncActionState = { state: string; error: string | null; message: string | null };
type ActionOptions<T> = {
  pending?: string;
  successMessage?: string | null | ((_result: T) => string | null | undefined);
  action: () => T | Promise<T>;
  onError?: (_error: unknown) => boolean;
};

export const idleActionState: AsyncActionState = { state: "idle", error: null, message: null };

export function useAsyncAction(initialState: AsyncActionState = idleActionState) {
  const [actionState, setActionState] = useState<AsyncActionState>(initialState);
  const generationRef = useRef(0);
  const mountedRef = useRef(false);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      generationRef.current += 1;
    };
  }, []);

  const runAction = useCallback(
    async <T>({ pending = "saving", successMessage = null, action, onError }: ActionOptions<T>): Promise<T | undefined> => {
      const generation = ++generationRef.current;
      setActionState({ state: pending, error: null, message: null });
      try {
        const result = await action();
        if (!mountedRef.current || generation !== generationRef.current) return undefined;
        const message = typeof successMessage === "function" ? successMessage(result) : successMessage;
        setActionState({ state: "idle", error: null, message: message ?? null });
        return result;
      } catch (error) {
        if (!mountedRef.current || generation !== generationRef.current) return undefined;
        if (onError?.(error)) {
          setActionState(idleActionState);
          return undefined;
        }
        setActionState({ state: "error", error: errorMessage(error, "Unknown error."), message: null });
        return undefined;
      }
    },
    [],
  );

  const resetAction = useCallback(() => {
    generationRef.current += 1;
    setActionState(idleActionState);
  }, []);

  return { actionState, setActionState, runAction, resetAction };
}
