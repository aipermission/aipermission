package console

import "testing"

func TestManualInputBatchRetainsConsoleOwnedStreamBoundary(t *testing.T) {
	for _, input := range []string{"pwd\n", "pwd\nhostname\n", "nano file\npwd\n", "\x1b[A\npwd\n"} {
		t.Run(input, func(t *testing.T) {
			session := &managedConsoleSession{rawTranscript: "root@worker:~# "}
			boundary := &manualInputBoundary{startOffset: 9007199254740993, resumePrompt: "operator@worker:/work$"}
			prepared := session.prepareManualInputLocked(input, boundary)
			if len(prepared.commands) != 1 {
				t.Fatalf("batch produced %d records: %#v", len(prepared.commands), prepared)
			}
			command := prepared.commands[0]
			if command.StartOffset != boundary.startOffset || command.ResumePrompt != boundary.resumePrompt {
				t.Fatalf("parser replaced caller-owned stream context: %#v", command)
			}
		})
	}
}
