import { useEffect, useRef } from "react";

export function createRequestGuard(initialScope = "") {
  let active = true;
  let lifecycle = 0;
  let scope = initialScope;
  const versions = new Map();
  const controllers = new Map();

  function abortChannel(channel) {
    controllers.get(channel)?.abort();
    controllers.delete(channel);
  }

  function abortAll() {
    for (const controller of controllers.values()) controller.abort();
    controllers.clear();
  }

  return {
    activate() {
      active = true;
    },
    setScope(nextScope) {
      if (scope === nextScope) return;
      abortAll();
      scope = nextScope;
      lifecycle += 1;
      versions.clear();
    },
    begin(channel) {
      abortChannel(channel);
      const requestLifecycle = lifecycle;
      const requestScope = scope;
      const version = (versions.get(channel) || 0) + 1;
      const controller = new AbortController();
      versions.set(channel, version);
      controllers.set(channel, controller);
      return {
        signal: controller.signal,
        complete() {
          if (controllers.get(channel) === controller) controllers.delete(channel);
        },
        isCurrent() {
          return (
            active &&
            !controller.signal.aborted &&
            lifecycle === requestLifecycle &&
            scope === requestScope &&
            versions.get(channel) === version
          );
        },
      };
    },
    invalidate(channel) {
      abortChannel(channel);
      versions.set(channel, (versions.get(channel) || 0) + 1);
    },
    dispose() {
      abortAll();
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
