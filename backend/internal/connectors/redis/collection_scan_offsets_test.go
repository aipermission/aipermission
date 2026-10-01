package redisconnector

import (
	"math"
	"strings"
	"testing"
)

func TestScanCollectionRejectsUnsafeOffsetsBeforeDispatch(t *testing.T) {
	for _, kind := range []string{"hash", "set"} {
		for _, test := range []struct {
			name   string
			offset int64
		}{
			{"negative", -1},
			{"scan_page_overflow", maxRESPArrayItems + 1},
			{"int32_boundary", math.MaxInt32},
			{"int64_boundary", math.MaxInt64},
		} {
			t.Run(kind+"/"+test.name, func(t *testing.T) {
				preview := newCollectionPreview(kind, 100)
				position := redisPreviewPosition{cursor: "0", offset: test.offset}
				if result, err := readScanCollectionPreview(nil, preview, "key", 10, position); err == nil || !strings.Contains(err.Error(), "offset") || result != nil {
					t.Fatalf("unsafe offset reached dispatch: offset=%d result=%#v err=%v", test.offset, result, err)
				}
			})
		}
	}
}
