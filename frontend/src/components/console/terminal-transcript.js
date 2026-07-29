export function syncTerminalTranscript(terminal, lastTranscriptRef, transcript) {
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
