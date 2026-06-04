import "@xterm/xterm/css/xterm.css";
import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import { useEffect, useRef } from "react";

export function PtyConsole({ session, onInput, onResize, theme = "dark" }) {
  const containerRef = useRef(null);
  const terminalRef = useRef(null);
  const lastTranscriptRef = useRef("");

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
    const fit = new FitAddon();
    terminal.loadAddon(fit);
    terminal.open(containerRef.current);
    terminal.focus();
    fit.fit();
    onResize(terminal.cols, terminal.rows);

    const resizeObserver = new ResizeObserver(() => {
      fit.fit();
      onResize(terminal.cols, terminal.rows);
    });
    resizeObserver.observe(containerRef.current);

    const disposable = terminal.onData((data) => {
      onInput(data);
    });
    const focusHandler = () => terminal.focus();
    containerRef.current?.addEventListener("pointerdown", focusHandler);

    terminalRef.current = terminal;

    return () => {
      disposable.dispose();
      containerRef.current?.removeEventListener("pointerdown", focusHandler);
      resizeObserver.disconnect();
      terminal.dispose();
    };
  }, [theme]);

  useEffect(() => {
    const terminal = terminalRef.current;
    if (!terminal) return;
    const transcript = session.transcript || "";
    const previous = lastTranscriptRef.current;
    if (transcript.startsWith(previous)) {
      terminal.write(transcript.slice(previous.length));
    } else {
      terminal.clear();
      terminal.reset();
      terminal.write(transcript);
    }
    lastTranscriptRef.current = transcript;
    terminal.scrollToBottom();
  }, [session.transcript]);

  useEffect(() => {
    if (!session.transcript) {
      terminalRef.current?.clear();
      lastTranscriptRef.current = "";
    }
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
