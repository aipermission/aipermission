import { useCallback, useEffect, useRef, useState } from "react";
import { apiGet } from "../../lib/api";
import { errorMessage } from "../../lib/errors";
import { connectorCapacityResponse, type ConnectorCapacityReport } from "../../lib/gateway-contracts/connector-capacity-contract";
import { useRequestGuard } from "../../lib/request-guard";
import { subscribeUISessionInvalidation } from "../../lib/ui-session-events";

export function useConnectorCapacity() {
  const [state, setState] = useState<{ data: ConnectorCapacityReport | null; loading: boolean; error: string | null }>({
    data: null,
    loading: true,
    error: null,
  });
  const guard = useRequestGuard("connector-capacity");
  const busy = useRef(false);

  const refresh = useCallback(async () => {
    if (busy.current) return;
    busy.current = true;
    const request = guard.begin("usage");
    setState((previous) => ({ ...previous, loading: true }));
    try {
      const data = connectorCapacityResponse(await apiGet("/api/settings/connector-capacity", { signal: request.signal, timeoutMs: 4000 }));
      if (request.isCurrent()) setState({ data, loading: false, error: null });
    } catch (error) {
      if (request.isCurrent())
        setState((previous) => ({ ...previous, loading: false, error: errorMessage(error, "Could not load connector capacity.") }));
    } finally {
      if (request.isCurrent()) busy.current = false;
      request.complete();
    }
  }, [guard]);

  useEffect(() => {
    void refresh();
    const refreshVisible = () => {
      if (document.visibilityState === "visible") void refresh();
    };
    const timer = window.setInterval(refreshVisible, 30_000);
    window.addEventListener("focus", refreshVisible);
    document.addEventListener("visibilitychange", refreshVisible);
    const unsubscribe = subscribeUISessionInvalidation(() => {
      guard.invalidate("usage");
      busy.current = false;
      setState({ data: null, loading: false, error: "Session changed. Refresh after unlocking the database." });
    });
    return () => {
      window.clearInterval(timer);
      window.removeEventListener("focus", refreshVisible);
      document.removeEventListener("visibilitychange", refreshVisible);
      unsubscribe();
      guard.invalidate("usage");
      busy.current = false;
    };
  }, [guard, refresh]);

  return { ...state, refresh };
}
