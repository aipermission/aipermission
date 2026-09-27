import { act, fireEvent, render, screen } from "@testing-library/react";
import { StrictMode, useEffect } from "react";
import { expect, it, vi } from "vitest";
import { captureConsoleSessionRecovery, isConsoleSessionRecovery } from "./console-recovery";
import type { ConsoleOperation } from "../../../components/console/console-connector-view-types";
import type { NativeRecoveryRenderProps } from "./console-recovery-types";

type Operation = { open: boolean; state: string; error: string | null; label: string };
const runtimeTarget = { id: 19, name: "My runtime", connector_kind: "example" };

function fixture() {
  const callbacks: NativeRecoveryRenderProps<Operation>[] = [];
  const recover = vi.fn((error: unknown) =>
    error === "recover" ? { operation: { open: true, state: "idle", error: null, label: "Native payload" }, runtimeTarget } : null,
  );
  const captured = captureConsoleSessionRecovery<Operation>({
    kind: "example",
    recover,
    render(props) {
      callbacks.push(props);
      return (
        <>
          <p>
            {props.value.label}:{props.value.state}
          </p>
          <button onClick={() => props.onChange((current) => ({ ...current, state: `${current.state}+` }))}>Change</button>
          <button onClick={() => props.onChange((current) => ({ ...current, open: false }))}>Close</button>
          <button onClick={() => void props.onOperationComplete({ startConsoleSession: true })}>Complete</button>
        </>
      );
    },
  });
  const value = captured.operationFromError("recover", { operation: "new-session", target: runtimeTarget });
  if (!value) throw new Error("Missing fixture recovery");
  return { captured, recover, value, callbacks };
}

it("keeps native payloads private while preserving token identity through common envelopes", () => {
  const { captured, recover, value } = fixture();
  expect(isConsoleSessionRecovery(captured)).toBe(true);
  expect(isConsoleSessionRecovery({ ...captured })).toBe(false);
  for (const value of [undefined, null, false, [], () => {}]) expect(isConsoleSessionRecovery(value)).toBe(false);
  expect(value).not.toHaveProperty("label");
  expect(value.recovery).toEqual({});
  expect(Object.isFrozen(value.recovery)).toBe(true);
  expect(Object.isFrozen(captured)).toBe(true);
  expect(captured.operationFromError("unhandled", { operation: "new-session", target: runtimeTarget })).toBeNull();
  expect(recover).toHaveBeenCalledWith("recover", { operation: "new-session", target: runtimeTarget });
  render(<captured.Operations value={{ ...value }} credentials={[]} onChange={vi.fn()} onOperationComplete={vi.fn()} />);
  expect(screen.getByText("Native payload:idle")).toBeInTheDocument();
});

it("rejects closed native operations and blank connector kinds", () => {
  const definition = { kind: "example", recover: () => ({ operation: { open: false }, runtimeTarget }), render: () => null };
  expect(
    captureConsoleSessionRecovery(definition).operationFromError(null, { operation: "new-session", target: runtimeTarget }),
  ).toBeNull();
  expect(() => captureConsoleSessionRecovery({ ...definition, kind: " " })).toThrow("requires a connector kind");
});

it.each([null, {}, { recovery: {} }, { recovery: null }, { recovery: "unknown" }])(
  "does not render forged recovery envelopes %j",
  (extra) => {
    const { captured, value } = fixture();
    render(
      <captured.Operations
        value={{ ...value, recovery: undefined, ...extra }}
        credentials={[]}
        onChange={vi.fn()}
        onOperationComplete={vi.fn()}
      />,
    );
    expect(screen.queryByText(/Native payload/)).not.toBeInTheDocument();
  },
);

it("keeps functional native updates current, scopes common updates, and reports the original retry runtime", () => {
  const { captured, value } = fixture();
  const onChange = vi.fn();
  const onComplete = vi.fn();
  render(
    <StrictMode>
      <captured.Operations value={value} credentials={[]} onChange={onChange} onOperationComplete={onComplete} />
    </StrictMode>,
  );
  fireEvent.click(screen.getByText("Change"));
  fireEvent.click(screen.getByText("Change"));
  expect(screen.getByText("Native payload:idle++")).toBeInTheDocument();
  const update: unknown = onChange.mock.calls[1][0];
  if (typeof update !== "function") throw new Error("Expected common functional update");
  expect(update(value)).toMatchObject({ open: true, state: "idle++" });
  const other: ConsoleOperation = { open: true, connector_kind: "other" };
  expect(update(other)).toBe(other);
  fireEvent.click(screen.getByText("Complete"));
  expect(onComplete).toHaveBeenCalledWith({ startConsoleSession: true }, { connector_kind: "example", runtimeTarget });
  fireEvent.click(screen.getByText("Close"));
  const close: unknown = onChange.mock.calls[2][0];
  if (typeof close !== "function") throw new Error("Expected close update");
  expect(close(value)).toMatchObject({ open: false });
});

it("resets native state on replacement and retires callbacks after replacement, close and unmount", async () => {
  const { captured, value, callbacks } = fixture();
  const onChange = vi.fn();
  const onComplete = vi.fn();
  const props = { credentials: [], onChange, onOperationComplete: onComplete };
  const { rerender, unmount } = render(<captured.Operations {...props} value={value} />);
  const old = callbacks.at(-1);
  if (!old) throw new Error("Missing native callbacks");
  fireEvent.click(screen.getByText("Change"));
  const replacement = captured.operationFromError("recover", { operation: "new-session", target: runtimeTarget });
  if (!replacement) throw new Error("Missing replacement");
  rerender(<captured.Operations {...props} value={replacement} />);
  expect(screen.getByText("Native payload:idle")).toBeInTheDocument();
  onChange.mockClear();
  await act(async () => {
    old.onChange({ open: false, state: "error", error: "stale", label: "Wrong" });
    await old.onOperationComplete({ startConsoleSession: true });
  });
  expect(onChange).not.toHaveBeenCalled();
  expect(onComplete).not.toHaveBeenCalled();
  const latest = callbacks.at(-1);
  if (!latest) throw new Error("Missing latest native callbacks");
  rerender(<captured.Operations {...props} value={{ ...replacement, open: false }} />);
  await act(async () => {
    latest.onChange({ open: false, state: "error", error: "stale", label: "Wrong" });
    await latest.onOperationComplete({ startConsoleSession: true });
  });
  expect(onChange).not.toHaveBeenCalled();
  expect(onComplete).not.toHaveBeenCalled();
  rerender(<captured.Operations {...props} value={replacement} />);
  const last = callbacks.at(-1);
  if (!last) throw new Error("Missing final native callbacks");
  unmount();
  await act(async () => {
    last.onChange({ open: false, state: "error", error: "stale", label: "Wrong" });
    await last.onOperationComplete({ startConsoleSession: true });
  });
  expect(onChange).not.toHaveBeenCalled();
  expect(onComplete).not.toHaveBeenCalled();
});

it("retires completion synchronously when close and completion share a React batch", async () => {
  const { captured, value, callbacks } = fixture();
  const onComplete = vi.fn();
  render(<captured.Operations value={value} credentials={[]} onChange={vi.fn()} onOperationComplete={onComplete} />);
  const latest = callbacks.at(-1);
  if (!latest) throw new Error("Missing native callbacks");
  await act(async () => {
    latest.onChange((current) => ({ ...current, open: false }));
    await latest.onOperationComplete({ startConsoleSession: true });
  });
  expect(onComplete).not.toHaveBeenCalled();
});

it("does not reactivate callbacks retained by the retired StrictMode effect setup", async () => {
  const callbacks: NativeRecoveryRenderProps<Operation>[] = [];
  function NativeDialog(props: NativeRecoveryRenderProps<Operation>) {
    useEffect(() => {
      callbacks.push(props);
    }, [props]);
    return <p>Native dialog</p>;
  }
  const captured = captureConsoleSessionRecovery<Operation>({
    kind: "example",
    recover: () => ({ operation: { open: true, state: "idle", error: null, label: "Native" }, runtimeTarget }),
    render: (props) => <NativeDialog {...props} />,
  });
  const value = captured.operationFromError(null, { operation: "new-session", target: runtimeTarget });
  if (!value) throw new Error("Missing native recovery");
  const onComplete = vi.fn();
  const onChange = vi.fn();
  render(
    <StrictMode>
      <captured.Operations value={value} credentials={[]} onChange={onChange} onOperationComplete={onComplete} />
    </StrictMode>,
  );
  const first = callbacks[0];
  const latest = callbacks.at(-1);
  if (!first || !latest || first === latest) throw new Error("Expected retired and current effect setups");
  await act(async () => {
    first.onChange((current) => ({ ...current, state: "stale" }));
    await first.onOperationComplete({ startConsoleSession: true });
  });
  expect(onChange).not.toHaveBeenCalled();
  expect(onComplete).not.toHaveBeenCalled();
  await act(async () => {
    await latest.onOperationComplete({ startConsoleSession: true });
  });
  expect(onComplete).toHaveBeenCalledTimes(1);
});
