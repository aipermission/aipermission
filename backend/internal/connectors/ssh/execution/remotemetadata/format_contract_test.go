package remotemetadata

import (
	"strings"
	"testing"
)

func TestRemoteMetadataWireRejectsMalformedUnsignedFields(t *testing.T) {
	for _, input := range []string{
		"", "gnu 81c0 0", "gnu 81c0 0 0 extra", "unknown 81c0 0 0",
		"gnu invalid 0 0", "gnu 100000000 0 0", "bsd 8 0 0",
		"gnu 81c0 -1 0", "gnu 81c0 4294967296 0", "gnu 81c0 0 -1", "gnu 81c0 0 4294967296",
	} {
		t.Run(input, func(t *testing.T) {
			if _, err := parse(input); err == nil {
				t.Fatalf("malformed metadata accepted: %q", input)
			}
		})
	}
	got, err := parse("gnu 81c0 4294967295 4294967295")
	if err != nil || got.UID != 4294967295 || got.GID != 4294967295 || got.Mode.Perm() != 0o700 {
		t.Fatalf("exact unsigned max IDs = %#v, %v", got, err)
	}
}

func TestRemoteMetadataLimitIncludesExactBoundary(t *testing.T) {
	const record = "gnu 81c0 0 0"
	for _, size := range []int{maxOutputBytes, maxOutputBytes + 1} {
		output := record + strings.Repeat(" ", size-len(record))
		session := newFakeRemoteMetadataSession([][]byte{[]byte(output[:size-1]), []byte(output[size-1:])}, false)
		got, err := Read(t.Context(), session, "stat")
		if size == maxOutputBytes {
			if err != nil || got.Mode.Perm() != 0o700 {
				t.Fatalf("exact limit rejected: %#v, %v", got, err)
			}
		} else if err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("over limit accepted: %#v, %v", got, err)
		}
	}
}
