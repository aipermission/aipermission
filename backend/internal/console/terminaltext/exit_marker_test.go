package terminaltext

import "testing"

func TestCommandExitMarkerFrames(t *testing.T) {
	for _, test := range []struct {
		name, segment, output              string
		truncated, found, complete, failed bool
		exitCode                           int
	}{
		{"absent", "partial output", "partial output", false, false, false, false, 1},
		{"partial", "output\nmarker:7", "output\nmarker:7", false, true, false, false, 1},
		{"complete", "output\nmarker:7\n", "output", false, true, true, false, 7},
		{"crlf", "output\r\nmarker:0\r\n", "output\n", false, true, true, false, 0},
		{"missing-newline", "marker:0\n", "marker:0\n", false, false, false, false, 1},
		{"truncated", "marker:0\n", "", true, true, true, false, 0},
		{"invalid", "output\nmarker:x\n", "output", false, true, false, true, 1},
		{"empty", "output\nmarker:\n", "output", false, true, false, true, 1},
		{"overflow", "output\nmarker:99999999999999999999999999999\n", "output", false, true, false, true, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			output, exitCode, found, complete, err := CommandExitMarker(test.segment, "marker", test.truncated)
			if output != test.output || exitCode != test.exitCode || found != test.found || complete != test.complete || (err != nil) != test.failed {
				t.Fatalf("marker projection: %q %d %v %v %v", output, exitCode, found, complete, err)
			}
		})
	}
}
