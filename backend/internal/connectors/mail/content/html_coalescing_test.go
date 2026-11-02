package mailcontent

import (
	"runtime"
	"strings"
	"testing"
)

func TestHTMLIgnoredTagsDoNotCauseQuadraticTextAllocation(t *testing.T) {
	const repetitions = 20000
	for _, prefix := range []string{"", "<table>"} {
		source := prefix + strings.Repeat("a</x>", repetitions)
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		text, err := HTMLToText(source)
		runtime.ReadMemStats(&after)
		if err != nil || text != strings.Repeat("a", repetitions) {
			t.Fatalf("coalesced text changed: prefix=%q bytes=%d error=%v", prefix, len(text), err)
		}
		// Generous fixed ceiling for this sub-128KiB fixture; old coalescing
		// copied accumulated text on every ignored end tag (over 200MiB).
		if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 32<<20 {
			t.Fatalf("HTML text coalescing allocated %d bytes for %d input bytes", allocated, len(source))
		}
	}
}
