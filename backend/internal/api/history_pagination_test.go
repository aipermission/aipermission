package api

import (
	"net/http"
	"net/url"
	"testing"
)

func TestHistoryCursorPaginationIsStableAcrossNewRows(t *testing.T) {
	fixture := newAPITestFixture(t)
	createdAt := "2026-09-05T12:00:00.000Z"
	ids := make([]int64, 0, 4)
	for sourceID := int64(1); sourceID <= 4; sourceID++ {
		result, err := fixture.db.Exec(`
			INSERT INTO history_entries (
				source_ref_type, source_ref_id, connector_kind, activity_type,
				status, title, created_at, updated_at
			) VALUES ('cursor_test', ?, 'test', 'action', 'completed', 'cursor row', ?, ?)`,
			sourceID,
			createdAt,
			createdAt,
		)
		if err != nil {
			t.Fatalf("insert history row: %v", err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			t.Fatalf("history row id: %v", err)
		}
		ids = append(ids, id)
	}

	firstResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/history?connector_kind=test&limit=2", "", nil)
	if firstResponse.Code != http.StatusOK {
		t.Fatalf("first history page failed: %d %s", firstResponse.Code, firstResponse.Body.String())
	}
	firstPage := decodeRouteResponse[historyPageResponse](t, firstResponse.Body.Bytes())
	if firstPage.Total == nil || *firstPage.Total != 4 {
		t.Fatalf("first page total = %#v, want 4", firstPage.Total)
	}
	if !firstPage.HasMore || firstPage.NextCursor == "" || len(firstPage.Items) != 2 {
		t.Fatalf("unexpected first cursor page: %#v", firstPage)
	}
	if firstPage.Items[0].ID != ids[3] || firstPage.Items[1].ID != ids[2] {
		t.Fatalf("first page ids = [%d %d], want [%d %d]", firstPage.Items[0].ID, firstPage.Items[1].ID, ids[3], ids[2])
	}

	if _, err := fixture.db.Exec(`
		INSERT INTO history_entries (
			source_ref_type, source_ref_id, connector_kind, activity_type,
			status, title, created_at, updated_at
		) VALUES ('cursor_test', 5, 'test', 'action', 'completed', 'new cursor row',
			'2026-09-05T12:01:00.000Z', '2026-09-05T12:01:00.000Z')`); err != nil {
		t.Fatalf("insert newer history row: %v", err)
	}

	secondResponse := performJSON(
		fixture.server.Handler(),
		http.MethodGet,
		"/api/history?connector_kind=test&limit=2&cursor="+url.QueryEscape(firstPage.NextCursor),
		"",
		nil,
	)
	if secondResponse.Code != http.StatusOK {
		t.Fatalf("second history page failed: %d %s", secondResponse.Code, secondResponse.Body.String())
	}
	secondPage := decodeRouteResponse[historyPageResponse](t, secondResponse.Body.Bytes())
	if secondPage.Total != nil {
		t.Fatalf("cursor page should omit the optional total, got %d", *secondPage.Total)
	}
	if secondPage.HasMore || secondPage.NextCursor != "" || len(secondPage.Items) != 2 {
		t.Fatalf("unexpected second cursor page: %#v", secondPage)
	}
	if secondPage.Items[0].ID != ids[1] || secondPage.Items[1].ID != ids[0] {
		t.Fatalf("second page ids = [%d %d], want [%d %d]", secondPage.Items[0].ID, secondPage.Items[1].ID, ids[1], ids[0])
	}
}

func TestHistoryCursorPaginationValidatesInputsAndOptionalTotal(t *testing.T) {
	fixture := newAPITestFixture(t)

	for _, path := range []string{
		"/api/history?offset=0",
		"/api/history?cursor=not-base64!",
		"/api/history?include_total=perhaps",
	} {
		response := performJSON(fixture.server.Handler(), http.MethodGet, path, "", nil)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d, want 400: %s", path, response.Code, response.Body.String())
		}
	}

	response := performJSON(fixture.server.Handler(), http.MethodGet, "/api/history?include_total=false", "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("history page without total failed: %d %s", response.Code, response.Body.String())
	}
	page := decodeRouteResponse[historyPageResponse](t, response.Body.Bytes())
	if page.Total != nil {
		t.Fatalf("include_total=false returned a total: %d", *page.Total)
	}
}
