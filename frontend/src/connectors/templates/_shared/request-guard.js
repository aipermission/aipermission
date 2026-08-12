import { useEffect, useRef } from "react";

export function createRequestGuard(initialScope = "") {
  let active = true;
  let lifecycle = 0;
  let scope = initialScope;
  const versions = new Map();

  return {
    activate() {
      active = true;
    },
    setScope(nextScope) {
      if (scope === nextScope) return;
      scope = nextScope;
      lifecycle += 1;
      versions.clear();
    },
    begin(channel) {
      const requestLifecycle = lifecycle;
      const requestScope = scope;
      const version = (versions.get(channel) || 0) + 1;
      versions.set(channel, version);
      return {
        isCurrent() {
          return active && lifecycle === requestLifecycle && scope === requestScope && versions.get(channel) === version;
        },
      };
    },
    invalidate(channel) {
      versions.set(channel, (versions.get(channel) || 0) + 1);
    },
    dispose() {
      active = false;
      lifecycle += 1;
      versions.clear();
    },
  };
}

export function useRequestGuard(scope) {
  const guardRef = useRef(null);
  if (!guardRef.current) guardRef.current = createRequestGuard(scope);
  const guard = guardRef.current;

  useEffect(() => {
    guard.setScope(scope);
  }, [guard, scope]);
  useEffect(() => {
    guard.activate();
    return () => guard.dispose();
  }, [guard]);

  return guard;
}
