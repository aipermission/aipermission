type TranscriptTerminal = {
  write: (_value: string) => void;
  clear: () => void;
  reset: () => void;
  scrollToBottom: () => void;
};

export function syncTerminalTranscript(terminal: TranscriptTerminal, lastTranscriptRef: { current: string }, transcript: string): void {
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
}
