import { useCallback, useRef, useState } from "react";
import { useRequestGuard } from "../lib/request-guard";

export function useUnlockLifecycleMutation(onUnlocked) {
  const requests = useRequestGuard("unlock:lifecycle-mutation");
  const activeRef = useRef("");
  const [activeMutation, setActiveMutation] = useState("");

  const runMutation = useCallback(
    async (name, execute) => {
      if (activeRef.current) throw new Error("Another database operation is already running.");
      const request = requests.begin(name);
      activeRef.current = name;
      setActiveMutation(name);
      try {
        const result = await execute(request.signal);
        if (request.isCurrent()) await onUnlocked(request.signal);
        if (!request.isCurrent()) return result;
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
