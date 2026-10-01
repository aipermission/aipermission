package rolejournal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"testing"

	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
)

func historyResource(t *testing.T, id, targetID int64) resourcecontract.CredentialResource {
	t.Helper()
	resource := resourceTest(t)
	entry, err := parseResource(resource)
	if err != nil {
		t.Fatal(err)
	}
	record := entry.Record
	record.Intent.OperationID = fmt.Sprintf("%032x", id)
	record.Intent.Anchor.TargetID = targetID
	resource.ID, resource.Name = id, resourceName(record.Intent)
	resource.Fingerprint = intentDigest(record.Intent)
	return encodeTestRecord(t, resource, record)
}

func TestHistoryForTargetKeysetPages(t *testing.T) {
	store := newMemoryStore()
	for id := int64(HistoryPageLimit*2 + 1); id > 0; id-- {
		store.listed = append(store.listed, historyResource(t, id, 1))
	}
	store.listed = append(store.listed, historyResource(t, math.MaxInt64, 2))
	journal := New(store)
	after := int64(0)
	for pageIndex := range 3 {
		page, err := journal.HistoryForTarget(t.Context(), 1, after)
		if err != nil {
			t.Fatal(err)
		}
		wantCount := HistoryPageLimit
		if pageIndex == 2 {
			wantCount = 1
		}
		if len(page.Entries) != wantCount || page.HasMore != (pageIndex < 2) {
			t.Fatalf("page %d: %+v", pageIndex, page)
		}
		for i, entry := range page.Entries {
			if wantID := after + int64(i) + 1; entry.ResourceID != wantID || entry.Record.Intent.Anchor.TargetID != 1 {
				t.Fatalf("page %d entry %d: %+v, want id %d", pageIndex, i, entry, wantID)
			}
		}
		after = page.Entries[len(page.Entries)-1].ResourceID
		if page.HasMore && page.NextAfterResourceID != strconv.FormatInt(after, 10) || !page.HasMore && page.NextAfterResourceID != "" {
			t.Fatalf("page %d cursor %q", pageIndex, page.NextAfterResourceID)
		}
	}
	if store.creates != 0 || store.updates != 0 || store.reads != 0 {
		t.Fatalf("inspection accessed mutable or secret ports: %+v", store)
	}
}

func TestHistoryForTargetEmptyAndExactBoundary(t *testing.T) {
	store := newMemoryStore()
	for id := int64(1); id <= HistoryPageLimit; id++ {
		store.listed = append(store.listed, historyResource(t, id, 1))
	}
	journal := New(store)
	for _, tc := range []struct {
		name          string
		target, after int64
		count         int
	}{
		{"exact page", 1, 0, HistoryPageLimit},
		{"strictly after", 1, 1, HistoryPageLimit - 1},
		{"exhausted", 1, HistoryPageLimit, 0},
		{"other target", 2, 0, 0},
		{"maximum cursor", 1, math.MaxInt64, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page, err := journal.HistoryForTarget(t.Context(), tc.target, tc.after)
			if err != nil || len(page.Entries) != tc.count || page.Entries == nil || page.HasMore || page.NextAfterResourceID != "" {
				t.Fatalf("page %+v, error %v", page, err)
			}
			encoded, err := json.Marshal(page)
			if err != nil {
				t.Fatal(err)
			}
			if tc.count == 0 && string(encoded) != `{"entries":[],"has_more":false,"next_after_resource_id":""}` {
				t.Fatalf("empty page serialized as %s", encoded)
			}
		})
	}
}

func TestHistoryForTargetPreservesLargeResourceID(t *testing.T) {
	store := newMemoryStore()
	store.listed = []resourcecontract.CredentialResource{historyResource(t, math.MaxInt64, 1)}
	page, err := New(store).HistoryForTarget(t.Context(), 1, math.MaxInt64-1)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Entries []struct {
			ResourceID string `json:"resource_id"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(encoded, &wire); err != nil || len(wire.Entries) != 1 || wire.Entries[0].ResourceID != "9223372036854775807" {
		t.Fatalf("resource ID lost precision: %s, %v", encoded, err)
	}
}

func TestHistoryForTargetRejectsInvalidScope(t *testing.T) {
	for _, tc := range []struct {
		name          string
		ctx           context.Context
		target, after int64
	}{
		{"nil context", nil, 1, 0},
		{"missing target", t.Context(), 0, 0},
		{"negative target", t.Context(), -1, 0},
		{"negative cursor", t.Context(), 1, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A nil journal would fail or panic if validation touched storage.
			if _, err := (*Journal)(nil).HistoryForTarget(tc.ctx, tc.target, tc.after); err == nil {
				t.Fatal("accepted invalid scope")
			}
		})
	}
}

func TestHistoryForTargetPropagatesReadFailureAndCancellation(t *testing.T) {
	sentinel := errors.New("private storage detail")
	store := newMemoryStore()
	store.listErr = sentinel
	if _, err := New(store).HistoryForTarget(t.Context(), 1, 0); !errors.Is(err, sentinel) {
		t.Fatalf("storage cause lost: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := New(newMemoryStore()).HistoryForTarget(ctx, 1, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation cause lost: %v", err)
	}
	for _, journal := range []*Journal{nil, New(nil), New((*memoryStore)(nil))} {
		if _, err := journal.HistoryForTarget(t.Context(), 1, 0); err == nil {
			t.Fatal("missing store accepted")
		}
	}
}

func TestHistoryForTargetValidatesRecordsOutsideRequestedPage(t *testing.T) {
	for _, tc := range []struct {
		name string
		row  resourcecontract.CredentialResource
	}{
		{"other target", historyResource(t, 2, 2)},
		{"before cursor", historyResource(t, 1, 1)},
		{"beyond first page", historyResource(t, HistoryPageLimit+2, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newMemoryStore()
			store.listed = []resourcecontract.CredentialResource{tc.row}
			store.listed[0].PublicData = `{"version":1}`
			page, err := New(store).HistoryForTarget(t.Context(), 1, 1)
			if err == nil || len(page.Entries) != 0 || page.HasMore || page.NextAfterResourceID != "" {
				t.Fatalf("corrupt row hidden by page filter: %+v, %v", page, err)
			}
		})
	}
}
