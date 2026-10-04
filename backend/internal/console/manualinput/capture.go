package manualinput

import (
	"strings"
	"unicode/utf8"
)

// Capture observes submitted terminal input. Its caller owns synchronization and
// decides whether resulting records may be persisted or used to track output.
// The zero value is ready to consume input.
type Capture struct {
	line              string
	initialized       bool
	trusted           bool
	escapePending     bool
	escapeIntro       bool
	escapeOSC         bool
	lastWasCR         bool
	truncated         bool
	heredocTerminator string
	historyRecall     bool
}

func (c *Capture) Consume(data string) []Record {
	data = stripBracketedPasteMarkers(data)
	if !c.initialized {
		c.initialized = true
		c.trusted = true
	}
	records := []Record{}
	for _, r := range data {
		if c.consumeEscape(r) {
			continue
		}
		switch r {
		case '\x1b':
			c.trusted = false
			c.escapePending = true
			c.escapeIntro = true
			c.escapeOSC = false
		case '\r':
			records = append(records, c.finishLine()...)
			c.lastWasCR = true
		case '\n':
			if c.lastWasCR {
				c.lastWasCR = false
				continue
			}
			records = append(records, c.finishLine()...)
		case '\x03', '\x04':
			c.Reset()
		case '\b', '\x7f':
			c.line = trimLastRune(c.line)
			c.lastWasCR = false
		case '\t':
			c.appendRune(r)
			c.lastWasCR = false
		default:
			c.lastWasCR = false
			if r < 0x20 || r == 0x7f {
				c.trusted = false
				continue
			}
			c.appendRune(r)
		}
	}
	return records
}

func (c *Capture) consumeEscape(r rune) bool {
	if !c.escapePending {
		return false
	}
	if c.escapeIntro {
		c.escapeIntro = false
		if r == ']' {
			c.escapeOSC = true
		}
		if r != '[' && r != ']' {
			c.escapePending = false
		}
		return true
	}
	if c.escapeOSC {
		if r == '\a' {
			c.escapePending = false
			c.escapeOSC = false
		}
		return true
	}
	if r >= '@' && r <= '~' {
		if r == 'A' || r == 'B' {
			c.historyRecall = true
		}
		c.escapePending = false
	}
	return true
}

func (c *Capture) appendRune(r rune) {
	if len(c.line) >= BufferLimit {
		c.truncated = true
		return
	}
	c.line += string(r)
	if len(c.line) > BufferLimit {
		c.line = c.line[:BufferLimit]
		for len(c.line) > 0 && !utf8.ValidString(c.line) {
			c.line = c.line[:len(c.line)-1]
		}
		c.truncated = true
	}
}

func (c *Capture) finishLine() []Record {
	line := c.line
	trusted := c.trusted
	truncated := c.truncated
	c.line = ""
	c.initialized = true
	c.trusted = true
	c.truncated = false
	c.escapePending = false
	c.escapeIntro = false
	c.escapeOSC = false
	historyRecall := c.historyRecall
	c.historyRecall = false

	if c.heredocTerminator != "" {
		if strings.TrimSpace(line) == c.heredocTerminator {
			c.heredocTerminator = ""
		}
		return nil
	}

	command := strings.TrimSpace(line)
	if command == "" {
		if historyRecall {
			return []Record{{
				Command:                  "command recalled with arrow key",
				TrackingReason:           "history_recall_untracked",
				TrackOutput:              true,
				CompletionTrackingReason: "history_recall_untracked",
			}}
		}
		return nil
	}
	if !trusted {
		return []Record{{
			Command:        Preview(command, len(command) > PreviewLimit),
			TrackingReason: "untrusted_command_text",
		}}
	}

	record := classifyManualCommand(command, truncated)
	if terminator := heredocTerminator(command); terminator != "" {
		c.heredocTerminator = terminator
	}
	return []Record{record}
}

func stripBracketedPasteMarkers(data string) string {
	if !strings.Contains(data, "\x1b[") {
		return data
	}
	data = strings.ReplaceAll(data, "\x1b[200~", "")
	data = strings.ReplaceAll(data, "\x1b[201~", "")
	return data
}

func (c *Capture) Reset() {
	c.line = ""
	c.initialized = true
	c.trusted = true
	c.escapePending = false
	c.escapeIntro = false
	c.escapeOSC = false
	c.lastWasCR = false
	c.truncated = false
	c.heredocTerminator = ""
	c.historyRecall = false
}
