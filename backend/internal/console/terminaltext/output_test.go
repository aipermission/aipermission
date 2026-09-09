package terminaltext

import (
	"strings"
	"testing"
)

func TestPlainConsoleOutputRemovesMarkersAndANSI(t *testing.T) {
	input := "\x1b[32mroot@host:~#\x1b[0m\n>\n>\nroot@host:~# PS2=\n__aipermission_saved_ps2=${PS2-}\nPS2=\nhello\r\n\r\nworld\n__AIPERMISSION_EXIT_1_2__:0\nstty -echo\nstty sane 2>/dev/null || stty echo icanon opost 2>/dev/null || true\n"
	output := PlainOutput(input)

	if strings.Contains(output, "\x1b") {
		t.Fatalf("expected ANSI sequences to be removed: %q", output)
	}
	if strings.Contains(output, "__AIPERMISSION_EXIT") || strings.Contains(output, "stty -echo") || strings.Contains(output, "stty sane") {
		t.Fatalf("expected internal markers to be removed: %q", output)
	}
	if strings.Contains(output, "\n>") || strings.Contains(output, "__aipermission_saved_ps2") || strings.Contains(output, "PS2=") {
		t.Fatalf("expected shell prompt noise to be removed: %q", output)
	}
	if strings.Contains(output, "\n\n") {
		t.Fatalf("expected blank lines to be compacted away: %q", output)
	}
	if !strings.Contains(output, "hello") {
		t.Fatalf("expected command output to remain: %q", output)
	}
	if !strings.Contains(output, "world") {
		t.Fatalf("expected later command output to remain: %q", output)
	}
}

func TestTailStringByBytesKeepsUTF8Boundary(t *testing.T) {
	value := "abcé🙂def"
	got := TailStringByBytes(value, 7)
	if got != "🙂def" {
		t.Fatalf("expected UTF-8 safe tail, got %q", got)
	}
}
