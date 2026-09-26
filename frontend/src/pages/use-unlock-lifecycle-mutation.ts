import { useCallback, useRef, useState } from "react";
import { useRequestGuard } from "../lib/request-guard";

export function useUnlockLifecycleMutation(onUnlocked: (_signal: AbortSignal) => void | Promise<unknown>) {
  const requests = useRequestGuard("unlock:lifecycle-mutation");
  const activeRef = useRef("");
  const [activeMutation, setActiveMutation] = useState("");

  const runMutation = useCallback(
    async <Result,>(name: string, execute: (_signal: AbortSignal) => Promise<Result>): Promise<Result> => {
      if (activeRef.current) throw new Error("Another database operation is already running.");
      const request = requests.begin(name);
      activeRef.current = name;
      setActiveMutation(name);
      try {
        const result = await execute(request.signal);
        if (request.isCurrent()) await onUnlocked(request.signal);
        return result;
      } finally {
        if (request.isCurrent()) {
          activeRef.current = "";
          setActiveMutation("");
        }
        request.complete();
      }
    },
    [onUnlocked, requests],
  );

  return { activeMutation, runMutation };
}
