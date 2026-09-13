package archivepath

import "testing"

func TestTrackerDetectsPortablePathConflicts(t *testing.T) {
	tracker := NewTracker()
	tracker.Register([]string{"Reports", "Final. "})
	if got := tracker.ConflictIndex([]string{"reports", "final"}); got != 1 {
		t.Fatalf("case-folded conflict index = %d, want 1", got)
	}
	if got := tracker.ConflictIndex([]string{"Reports", "Final", "nested"}); got != 1 {
		t.Fatalf("file-prefix conflict index = %d, want 1", got)
	}
	if got := tracker.ConflictIndex([]string{"other", "file"}); got != -1 {
		t.Fatalf("unrelated path conflict index = %d", got)
	}
}
