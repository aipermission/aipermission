package api

import (
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
)

const maxHistoryCursorLength = 256

type historyCursor struct {
	CreatedAt string
	ID        int64
}

type historyPageRequest struct {
	Limit        int
	Query        string
	Cursor       *historyCursor
	IncludeTotal bool
}

type historyPageResponse struct {
	Items      []historyEntryRecord `json:"items"`
	Total      *int                 `json:"total,omitempty"`
	Limit      int                  `json:"limit"`
	NextCursor string               `json:"next_cursor,omitempty"`
	HasMore    bool                 `json:"has_more"`
}

func parseHistoryPageRequest(r *http.Request) (historyPageRequest, error) {
	if _, present := r.URL.Query()["offset"]; present {
		return historyPageRequest{}, errInvalidQuery("history offset pagination is not supported; use cursor")
	}
	limit, err := parsePageLimit(r)
	if err != nil {
		return historyPageRequest{}, err
	}
	cursor, err := decodeHistoryCursor(strings.TrimSpace(r.URL.Query().Get("cursor")))
	if err != nil {
		return historyPageRequest{}, err
	}
	includeTotal := cursor == nil
	if raw := strings.TrimSpace(r.URL.Query().Get("include_total")); raw != "" {
		includeTotal, err = strconv.ParseBool(raw)
		if err != nil {
			return historyPageRequest{}, errInvalidQuery("invalid include_total")
		}
	}
	return historyPageRequest{
		Limit:        limit,
		Query:        strings.TrimSpace(r.URL.Query().Get("q")),
		Cursor:       cursor,
		IncludeTotal: includeTotal,
	}, nil
}

func encodeHistoryCursor(cursor historyCursor) string {
	payload := cursor.CreatedAt + "\n" + strconv.FormatInt(cursor.ID, 10)
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func decodeHistoryCursor(raw string) (*historyCursor, error) {
	if raw == "" {
		return nil, nil
	}
	if len(raw) > maxHistoryCursorLength {
		return nil, errInvalidQuery("invalid history cursor")
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, errInvalidQuery("invalid history cursor")
	}
	parts := strings.Split(string(payload), "\n")
	if len(parts) != 2 || parts[0] == "" || len(parts[0]) > 64 {
		return nil, errInvalidQuery("invalid history cursor")
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || id < 1 {
		return nil, errInvalidQuery("invalid history cursor")
	}
	return &historyCursor{CreatedAt: parts[0], ID: id}, nil
}
