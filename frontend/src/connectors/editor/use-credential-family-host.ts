import { useCallback, useRef, useState } from "react";
import { idleActionState, type AsyncActionState } from "../../lib/use-async-action";
import type { CredentialFamilyCommands } from "./credential-family-types";

export function useCredentialFamilyHost() {
  const commands = useRef(new Map<string, CredentialFamilyCommands>());
  const activeKind = useRef("");
  const [state, setState] = useState(idleActionState);
  const [rowCounts, setRowCounts] = useState<ReadonlyMap<string, number>>(new Map());
  const register = useCallback((kind: string, value: CredentialFamilyCommands | null) => {
    if (value) commands.current.set(kind, value);
    else {
      commands.current.delete(kind);
      if (activeKind.current === kind) {
        activeKind.current = "";
        setState(idleActionState);
      }
    }
  }, []);
  const onOpen = useCallback((kind: string) => {
    activeKind.current = kind;
    setState(idleActionState);
    for (const [otherKind, value] of commands.current) if (otherKind !== kind) value.close();
  }, []);
  const onStateChange = useCallback((kind: string, next: AsyncActionState) => {
    if (kind === activeKind.current) setState(next);
  }, []);
  const onRowsChange = useCallback((kind: string, count: number | null) => {
    setRowCounts((current) => {
      if ((count === null && !current.has(kind)) || current.get(kind) === count) return current;
      const next = new Map(current);
      if (count === null) next.delete(kind);
      else next.set(kind, count);
      return next;
    });
  }, []);
  const openCreate = useCallback((kind: string) => commands.current.get(kind)?.openCreate(), []);
  return {
    state,
    busy: state.state !== "idle" && state.state !== "error",
    rowCounts,
    register,
    onOpen,
    onStateChange,
    onRowsChange,
    openCreate,
  };
}
