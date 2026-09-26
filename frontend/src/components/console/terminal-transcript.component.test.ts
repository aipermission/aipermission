import { expect, it, vi } from "vitest";

import { syncTerminalTranscript } from "./terminal-transcript";

it("writes only appended terminal output and keeps the view at the bottom", () => {
  const terminal = { write: vi.fn(), clear: vi.fn(), reset: vi.fn(), scrollToBottom: vi.fn() };
  const lastTranscriptRef = { current: "prompt> " };

  syncTerminalTranscript(terminal, lastTranscriptRef, "prompt> ls\nfile\n");

  expect(terminal.write).toHaveBeenCalledWith("ls\nfile\n");
  expect(terminal.clear).not.toHaveBeenCalled();
  expect(terminal.reset).not.toHaveBeenCalled();
  expect(lastTranscriptRef.current).toBe("prompt> ls\nfile\n");
  expect(terminal.scrollToBottom).toHaveBeenCalledOnce();
});

it("resets the terminal when the new transcript is not an extension", () => {
  const terminal = { write: vi.fn(), clear: vi.fn(), reset: vi.fn(), scrollToBottom: vi.fn() };
  const lastTranscriptRef = { current: "old session" };

  syncTerminalTranscript(terminal, lastTranscriptRef, "new session");

  expect(terminal.clear).toHaveBeenCalledOnce();
  expect(terminal.reset).toHaveBeenCalledOnce();
  expect(terminal.write).toHaveBeenCalledWith("new session");
  expect(lastTranscriptRef.current).toBe("new session");
  expect(terminal.scrollToBottom).toHaveBeenCalledOnce();
});
