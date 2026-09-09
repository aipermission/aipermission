package connectors

import "testing"

func TestFormatAndParseTargetRefRoundTrip(t *testing.T) {
	ref := FormatTargetRef("postgres", 42, 7)
	if ref != "postgres:42:7" {
		t.Fatalf("ref = %q", ref)
	}
	kind, targetID, profileID, ok := ParseTargetRef(ref)
	if !ok || kind != "postgres" || targetID != 42 || profileID != 7 {
		t.Fatalf("parse = %q %d %d ok=%v", kind, targetID, profileID, ok)
	}
}

func TestParseTargetRefRejectsInvalidRefs(t *testing.T) {
	for _, ref := range []string{"", "ssh:1", "postgres", "Postgres:1:2", "postgres:0:2", "postgres:1:0", "postgres:x:2"} {
		if _, _, _, ok := ParseTargetRef(ref); ok {
			t.Fatalf("expected %q to be rejected", ref)
		}
	}
}
