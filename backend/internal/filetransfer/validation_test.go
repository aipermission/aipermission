package filetransfer

import (
	"reflect"
	"testing"
)

func TestBatchAndTransferListFiltersShareNormalizationPolicy(t *testing.T) {
	for _, fixture := range []struct {
		input ListFilter
		want  ListFilter
	}{
		{input: ListFilter{Direction: " upload ", Status: " running ", TargetIDs: []int64{3, -1, 3, 7}, Query: " worker ", Limit: 101, Offset: -4, RuntimeID: 5},
			want: ListFilter{Direction: "upload", Status: "running", TargetIDs: []int64{3, 7}, Query: "worker", Limit: 50, Offset: 0, RuntimeID: 5}},
		{input: ListFilter{Direction: "invalid", Status: "unknown", Limit: 0, Offset: 2},
			want: ListFilter{Limit: 50, Offset: 2}},
		{input: ListFilter{Direction: "download", Status: "failed", Limit: 20, Offset: 3},
			want: ListFilter{Direction: "download", Status: "failed", Limit: 20, Offset: 3}},
	} {
		transfer := normalizeListFilter(fixture.input)
		batch := normalizeBatchListFilter(BatchListFilter(fixture.input))
		if !reflect.DeepEqual(transfer, fixture.want) || !reflect.DeepEqual(ListFilter(batch), fixture.want) {
			t.Fatalf("transfer = %#v, batch = %#v, want = %#v", transfer, batch, fixture.want)
		}
		if transfer.Limit < 1 || transfer.Limit > 100 || transfer.Offset < 0 {
			t.Fatalf("unsafe pagination after normalization: %#v", transfer)
		}
	}
}
