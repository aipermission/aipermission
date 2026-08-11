import "@xterm/xterm/css/xterm.css";
import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import { useEffect, useRef } from "react";
import { syncTerminalTranscript } from "./terminal-transcript";

export function PtyConsole({ session, onInput, onResize, theme = "dark" }) {
  const containerRef = useRef(null);
  const terminalRef = useRef(null);
  const lastTranscriptRef = useRef("");
  const latestTranscriptRef = useRef(session.transcript || "");
  const onInputRef = useRef(onInput);
  const onResizeRef = useRef(onResize);
  onInputRef.current = onInput;
  onResizeRef.current = onResize;

  useEffect(() => {
    const terminal = new Terminal({
      cursorBlink: true,
      convertEol: true,
      scrollback: 5000,
      fontFamily: '"JetBrains Mono", "Cascadia Code", "SFMono-Regular", Consolas, "Liberation Mono", Menlo, monospace',
      fontSize: 13,
      lineHeight: 1.65,
      theme: terminalTheme(theme),
    });
    const container = containerRef.current;
    const fit = new FitAddon();
    terminal.loadAddon(fit);
    terminal.open(container);
    terminal.focus();
    const fitAndResize = () => {
      fit.fit();
      onResizeRef.current(terminal.cols, terminal.rows);
    };
    fitAndResize();
    const frame = requestAnimationFrame(fitAndResize);
    const settleTimer = window.setTimeout(fitAndResize, 80);

    const resizeObserver = new ResizeObserver(() => {
      fitAndResize();
    });
    resizeObserver.observe(container);

    const disposable = terminal.onData((data) => {
      onInputRef.current(data);
    });
    const focusHandler = () => terminal.focus();
    container?.addEventListener("pointerdown", focusHandler);

    terminalRef.current = terminal;
    lastTranscriptRef.current = "";
    syncTerminalTranscript(terminal, lastTranscriptRef, latestTranscriptRef.current);

    return () => {
      disposable.dispose();
      container?.removeEventListener("pointerdown", focusHandler);
      cancelAnimationFrame(frame);
      window.clearTimeout(settleTimer);
      resizeObserver.disconnect();
      if (terminalRef.current === terminal) terminalRef.current = null;
      terminal.dispose();
    };
  }, [theme]);

  useEffect(() => {
    latestTranscriptRef.current = session.transcript || "";
    const terminal = terminalRef.current;
    if (!terminal) return;
    syncTerminalTranscript(terminal, lastTranscriptRef, session.transcript || "");
  }, [session.transcript]);

  return (
    <div className={`h-full min-h-0 p-3 ${theme === "light" ? "bg-white" : "bg-[#1e1e1e]"}`}>
      <div
        ref={containerRef}
        className={`h-full min-h-0 overflow-hidden rounded-md ${theme === "light" ? "terminal-surface-light" : "terminal-surface-dark"}`}
        onClick={() => terminalRef.current?.focus()}
      />
    </div>
  );
}

function terminalTheme(theme) {
  if (theme === "light") {
    return {
      background: "#ffffff",
      foreground: "#1c1917",
      cursor: "#065f46",
      selectionBackground: "#dbeafe",
      black: "#1c1917",
      red: "#dc2626",
      green: "#16a34a",
      yellow: "#d97706",
      blue: "#2563eb",
      magenta: "#9333ea",
      cyan: "#0891b2",
      white: "#f5f5f4",
    };
  }
  return {
    background: "#1e1e1e",
    foreground: "#d4d4d4",
    cursor: "#f8fafc",
    selectionBackground: "#536d8b",
    black: "#171717",
    red: "#ef4444",
    green: "#22c55e",
    yellow: "#f59e0b",
    blue: "#60a5fa",
    magenta: "#c084fc",
    cyan: "#22d3ee",
    white: "#f5f5f4",
  };
}
