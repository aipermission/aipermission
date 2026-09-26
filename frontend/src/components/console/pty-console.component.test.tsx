import { fireEvent, render } from "@testing-library/react";
import type { ITerminalOptions } from "@xterm/xterm";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { PtyConsole } from "./pty-console";

const { terminals, fits, FakeTerminal } = vi.hoisted(() => {
  const terminals: FakeTerminal[] = [];
  const fits: { fit: ReturnType<typeof vi.fn> }[] = [];
  class FakeTerminal {
    cols = 80;
    rows = 24;
    write = vi.fn();
    clear = vi.fn();
    reset = vi.fn();
    scrollToBottom = vi.fn();
    focus = vi.fn();
    dispose = vi.fn();
    loadAddon = vi.fn();
    open = vi.fn();
    dataListener: ((_data: string) => void) | null = null;
    dataDisposable = { dispose: vi.fn() };
    options: ITerminalOptions;
    constructor(options: ITerminalOptions) {
      this.options = options;
      terminals.push(this);
    }
    onData(listener: (_data: string) => void) {
      this.dataListener = listener;
      return this.dataDisposable;
    }
  }
  return { terminals, fits, FakeTerminal };
});

vi.mock("@xterm/xterm", () => ({ Terminal: FakeTerminal }));
vi.mock("@xterm/addon-fit", () => ({
  FitAddon: class {
    fit = vi.fn();
    constructor() {
      fits.push(this);
    }
  },
}));

class FakeResizeObserver {
  static instances: FakeResizeObserver[] = [];
  observe = vi.fn();
  disconnect = vi.fn();
  callback: () => void;
  constructor(callback: () => void) {
    this.callback = callback;
    FakeResizeObserver.instances.push(this);
  }
}

function activeTerminal() {
  const terminal = terminals.at(-1);
  if (!terminal) throw new Error("Expected an initialized terminal.");
  return terminal;
}

describe("shared PTY console", () => {
  beforeEach(() => {
    terminals.length = 0;
    fits.length = 0;
    FakeResizeObserver.instances.length = 0;
    vi.stubGlobal("ResizeObserver", FakeResizeObserver);
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("writes only appended transcript data and resets when the snapshot changes", () => {
    const props = { onInput: vi.fn(), onResize: vi.fn() };
    const { rerender } = render(<PtyConsole {...props} session={{ transcript: "hello" }} />);
    const terminal = activeTerminal();
    expect(terminal.write).toHaveBeenCalledWith("hello");
    rerender(<PtyConsole {...props} session={{ transcript: "hello world" }} />);
    expect(terminal.write).toHaveBeenLastCalledWith(" world");
    expect(terminal.reset).not.toHaveBeenCalled();
    rerender(<PtyConsole {...props} session={{ transcript: "replacement" }} />);
    expect(terminal.clear).toHaveBeenCalledOnce();
    expect(terminal.reset).toHaveBeenCalledOnce();
    expect(terminal.write).toHaveBeenLastCalledWith("replacement");
  });

  it("uses the current input and resize callbacks without recreating the terminal", () => {
    const firstInput = vi.fn();
    const secondInput = vi.fn();
    const resize = vi.fn();
    const { rerender } = render(<PtyConsole onInput={firstInput} onResize={vi.fn()} session={{}} />);
    const terminal = activeTerminal();
    rerender(<PtyConsole onInput={secondInput} onResize={resize} session={{}} />);
    terminal.dataListener?.("ls\r");
    FakeResizeObserver.instances[0]?.callback();
    expect(firstInput).not.toHaveBeenCalled();
    expect(secondInput).toHaveBeenCalledWith("ls\r");
    expect(resize).toHaveBeenCalledWith(80, 24);
    expect(terminals).toHaveLength(1);
  });

  it("disposes observers and the input listener when the terminal unmounts", () => {
    const { container, unmount } = render(<PtyConsole session={{}} onInput={vi.fn()} onResize={vi.fn()} />);
    const terminal = activeTerminal();
    const surface = container.querySelector(".terminal-surface-dark");
    if (!surface) throw new Error("Expected the terminal surface.");
    fireEvent.pointerDown(surface);
    expect(terminal.focus).toHaveBeenCalledTimes(2);
    unmount();
    expect(terminal.dataDisposable.dispose).toHaveBeenCalledOnce();
    expect(terminal.dispose).toHaveBeenCalledOnce();
    expect(FakeResizeObserver.instances[0]?.disconnect).toHaveBeenCalledOnce();
  });

  it("recreates the themed terminal while retaining the latest transcript", () => {
    const props = { onInput: vi.fn(), onResize: vi.fn(), session: { transcript: "latest" } };
    const { rerender } = render(<PtyConsole {...props} theme="dark" />);
    const first = activeTerminal();
    rerender(<PtyConsole {...props} theme="light" />);
    expect(first.dispose).toHaveBeenCalledOnce();
    expect(activeTerminal().options.theme?.background).toBe("#ffffff");
    expect(activeTerminal().write).toHaveBeenCalledWith("latest");
  });
});
