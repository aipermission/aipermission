package history

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHistoryCursorRoundTripAndBounds(t *testing.T) {
	want := cursor{CreatedAt: "2026-09-09T12:34:56.123Z", ID: 42}
	got, err := decodeCursor(encodeCursor(want))
	if err != nil || got == nil || *got != want {
		t.Fatalf("cursor round trip = %#v, err = %v", got, err)
	}
	for _, raw := range []string{
		"not-base64!",
		encodeCursor(cursor{CreatedAt: "", ID: 42}),
		encodeCursor(cursor{CreatedAt: want.CreatedAt, ID: 0}),
		strings.Repeat("x", maxHistoryCursorLength+1),
	} {
		if _, err := decodeCursor(raw); err == nil {
			t.Fatalf("expected invalid cursor %q", raw)
		}
	}
}

func TestHistoryPageRequestRejectsOffsetAndBoundsLimit(t *testing.T) {
	request := httptest.NewRequest("GET", "/api/history?offset=0", nil)
	if _, err := parsePageRequest(request); err == nil {
		t.Fatal("expected offset pagination rejection")
	}
	request = httptest.NewRequest("GET", "/api/history?limit=10000&include_total=false&q=term", nil)
	page, err := parsePageRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	if page.Limit != maxPageLimit || page.IncludeTotal || page.Query != "term" {
		t.Fatalf("page = %#v", page)
	}
}
