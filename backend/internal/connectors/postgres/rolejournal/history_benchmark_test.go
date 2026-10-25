package rolejournal

import (
	"strconv"
	"testing"
)

// This measures decoding/filtering, not SQLite I/O or a bounded storage scan.
// History pagination bounds the response, not the journal's inspection work.
func BenchmarkHistoryForTarget(b *testing.B) {
	for _, size := range []int{100, 1000, 10000} {
		b.Run(strconv.Itoa(size)+"-records", func(b *testing.B) {
			store := newMemoryStore()
			for id := 1; id <= size; id++ {
				store.listed = append(store.listed, historyResource(b, int64(id), 1))
			}
			journal := New(store)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				page, err := journal.HistoryForTarget(b.Context(), 1, 0)
				if err != nil || len(page.Entries) != HistoryPageLimit || !page.HasMore {
					b.Fatalf("unexpected bounded response: count=%d error=%v", len(page.Entries), err)
				}
			}
		})
	}
}
