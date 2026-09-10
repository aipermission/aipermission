package httptransport

import (
	"net/http/httptest"
	"testing"
)

func TestParsePageRequest(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		want      PageRequest
		wantError bool
	}{
		{name: "defaults", want: PageRequest{Limit: DefaultPageLimit}},
		{name: "values", query: "?limit=25&offset=10&q=%20report%20", want: PageRequest{Limit: 25, Offset: 10, Query: "report"}},
		{name: "clamps limit", query: "?limit=1000", want: PageRequest{Limit: MaxPageLimit}},
		{name: "rejects zero limit", query: "?limit=0", wantError: true},
		{name: "rejects invalid limit", query: "?limit=many", wantError: true},
		{name: "rejects negative offset", query: "?offset=-1", wantError: true},
		{name: "rejects invalid offset", query: "?offset=next", wantError: true},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "/items"+testCase.query, nil)
			got, err := ParsePageRequest(request)
			if testCase.wantError {
				if err == nil {
					t.Fatalf("expected parse error, got %#v", got)
				}
				return
			}
			if err != nil || got != testCase.want {
				t.Fatalf("page request = %#v, %v; want %#v", got, err, testCase.want)
			}
		})
	}
}

func TestMakePageResponse(t *testing.T) {
	page := PageRequest{Limit: 2, Offset: 4}
	withNext := MakePageResponse([]string{"a", "b"}, 9, page)
	if withNext.NextOffset == nil || *withNext.NextOffset != 6 {
		t.Fatalf("next offset = %#v", withNext.NextOffset)
	}
	last := MakePageResponse([]string{"a"}, 5, page)
	if last.NextOffset != nil {
		t.Fatalf("last page next offset = %d", *last.NextOffset)
	}
}
