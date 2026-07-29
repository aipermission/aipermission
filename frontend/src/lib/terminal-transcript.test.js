import assert from "node:assert/strict";
import test from "node:test";
import { syncTerminalTranscript } from "../components/console/terminal-transcript.js";

function fakeTerminal() {
  const calls = [];
  return {
    calls,
    write(value) {
      calls.push(["write", value]);
    },
    clear() {
      calls.push(["clear"]);
    },
    reset() {
      calls.push(["reset"]);
    },
    scrollToBottom() {
      calls.push(["scroll"]);
    },
  };
}

test("terminal transcript writes the full snapshot into a fresh terminal", () => {
  const terminal = fakeTerminal();
  const previous = { current: "" };

  syncTerminalTranscript(terminal, previous, "login banner\nroot@host:~# ");

  assert.deepEqual(terminal.calls, [
    ["write", "login banner\nroot@host:~# "],
    ["scroll"],
  ]);
});

test("terminal transcript appends output and resets for a replacement snapshot", () => {
  const terminal = fakeTerminal();
  const previous = { current: "root@host:~# " };

  syncTerminalTranscript(terminal, previous, "root@host:~# pwd\n/root\n");
  syncTerminalTranscript(terminal, previous, "new session\n$ ");

  assert.deepEqual(terminal.calls, [
    ["write", "pwd\n/root\n"],
    ["scroll"],
    ["clear"],
    ["reset"],
    ["write", "new session\n$ "],
    ["scroll"],
  ]);
});
