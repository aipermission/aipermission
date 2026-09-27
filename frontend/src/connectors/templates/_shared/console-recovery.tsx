import { useEffect, useRef, useState } from "react";
import type { ConsoleOperation, ConsoleOperationSlotProps } from "../../../components/console/console-connector-view-types";
import type { ConsoleRuntimeTarget } from "../../../components/use-gateway-resources";
import type {
  ConsoleSessionRecovery,
  NativeConsoleRecovery,
  NativeRecoveryOperation,
  NativeRecoveryRenderProps,
} from "./console-recovery-types";

const capturedRecoveries = new WeakSet<object>();

export function captureConsoleSessionRecovery<Operation extends NativeRecoveryOperation>(
  definition: NativeConsoleRecovery<Operation>,
): Readonly<ConsoleSessionRecovery> {
  if (!definition.kind.trim()) throw new Error("Console recovery requires a connector kind.");
  type Entry = { key: number; operation: Operation; runtimeTarget: ConsoleRuntimeTarget };
  const entries = new WeakMap<object, Entry>();
  let sequence = 0;

  function entryFor(value: ConsoleOperation) {
    const token = value.recovery;
    return value.connector_kind === definition.kind && token !== null && typeof token === "object" ? entries.get(token) : undefined;
  }

  function NativeRecoveryBody({ entry, props }: { entry: Entry; props: ConsoleOperationSlotProps }) {
    const [value, setValue] = useState(entry.operation);
    const current = useRef(value);
    const [lifetime, setLifetime] = useState(() => ({ active: true }));
    const ownership = useRef(lifetime);
    useEffect(() => {
      // Effect replay gets new ownership; retired callbacks never reactivate.
      if (!ownership.current.active) {
        const next = { active: current.current.open };
        ownership.current = next;
        setLifetime(next);
      }
      return () => {
        ownership.current.active = false;
      };
    }, []);

    const onChange: NativeRecoveryRenderProps<Operation>["onChange"] = (next) => {
      if (!lifetime.active) return;
      const operation = typeof next === "function" ? next(current.current) : next;
      current.current = operation;
      if (!operation.open) lifetime.active = false;
      setValue(operation);
      props.onChange((previous) =>
        entryFor(previous) === entry ? { ...previous, open: operation.open, state: operation.state, error: operation.error } : previous,
      );
    };
    const onOperationComplete: NativeRecoveryRenderProps<Operation>["onOperationComplete"] = async (result) => {
      if (!lifetime.active) return;
      await props.onOperationComplete(result, { connector_kind: definition.kind, runtimeTarget: entry.runtimeTarget });
    };
    return definition.render({ value, onChange, onOperationComplete });
  }

  const captured: ConsoleSessionRecovery = {
    kind: definition.kind,
    operationFromError(error, context) {
      const recovery = definition.recover(error, context);
      if (!recovery || !recovery.operation.open) return null;
      const token = Object.freeze({});
      sequence += 1;
      entries.set(token, { key: sequence, ...recovery });
      return {
        open: true,
        connector_kind: definition.kind,
        type: "recovery",
        state: recovery.operation.state,
        error: recovery.operation.error,
        recovery: token,
      };
    },
    Operations(props) {
      const entry = entryFor(props.value);
      return props.value.open && entry ? <NativeRecoveryBody key={entry.key} entry={entry} props={props} /> : null;
    },
  };
  capturedRecoveries.add(captured);
  return Object.freeze(captured);
}

export function isConsoleSessionRecovery(value: unknown): value is Readonly<ConsoleSessionRecovery> {
  return value !== null && typeof value === "object" && capturedRecoveries.has(value);
}
