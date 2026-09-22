package timeformat

import (
	"testing"
	"time"
)

func TestUTCIsFixedWidthAndChronologicallySortable(t *testing.T) {
	older := UTC(time.Date(2026, 1, 1, 0, 0, 0, 100_000_000, time.FixedZone("offset", 3600)))
	newer := UTC(time.Date(2025, 12, 31, 23, 0, 0, 110_000_000, time.UTC))
	if older != "2025-12-31T23:00:00.100000000Z" {
		t.Fatalf("normalized timestamp = %q", older)
	}
	if len(older) != len(newer) || older >= newer {
		t.Fatalf("timestamps are not fixed-width chronological values: older=%q newer=%q", older, newer)
	}
}

func TestUTCPreservesSubMillisecondEventOrder(t *testing.T) {
	first := UTC(time.Date(2026, 1, 1, 0, 0, 0, 100_000_001, time.UTC))
	second := UTC(time.Date(2026, 1, 1, 0, 0, 0, 100_999_999, time.UTC))
	if first == second || first >= second {
		t.Fatalf("event timestamps lost chronological precision: first=%q second=%q", first, second)
	}
}

func TestPreciseUTCPreservesSubMillisecondRevisions(t *testing.T) {
	first := PreciseUTC(time.Date(2026, 1, 1, 0, 0, 0, 100, time.UTC))
	second := PreciseUTC(time.Date(2026, 1, 1, 0, 0, 0, 101, time.UTC))
	if first == second || first >= second {
		t.Fatalf("precise revisions are not ordered: first=%q second=%q", first, second)
	}
}
