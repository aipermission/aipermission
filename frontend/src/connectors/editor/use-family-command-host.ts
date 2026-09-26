import { useCallback, useRef, useState } from "react";
import { idleActionState, type AsyncActionState } from "../../lib/use-async-action";

export function useFamilyCommandHost<Commands extends { close: () => void }>() {
  const commands = useRef(new Map<string, Commands>());
  const activeKind = useRef("");
  const [state, setState] = useState(idleActionState);
  const register = useCallback((kind: string, value: Commands | null) => {
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
  return { commands, state, busy: state.state !== "idle" && state.state !== "error", register, onOpen, onStateChange };
}
