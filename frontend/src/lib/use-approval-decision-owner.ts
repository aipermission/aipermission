import { useMemo, useRef } from "react";
import type { createRequestGuard } from "./request-guard";

type Requests = ReturnType<typeof createRequestGuard>;
type Request = ReturnType<Requests["begin"]>;

export function useApprovalDecisionOwner(requests: Requests, channel: string) {
  const active = useRef<Request | null>(null);
  return useMemo(
    () => ({
      isPending: () => Boolean(active.current?.isCurrent()),
      begin() {
        if (active.current?.isCurrent()) return null;
        const request = requests.begin(channel);
        active.current = request;
        return request;
      },
      complete(request: Request) {
        if (active.current === request) active.current = null;
        request.complete();
      },
    }),
    [requests, channel],
  );
}
