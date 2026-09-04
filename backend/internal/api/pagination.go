package api

import (
	"net/http"
	"strconv"
	"strings"
)

const (
	defaultPageLimit = 50
	maxPageLimit     = 100
)

type pageRequest struct {
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
	Query  string `json:"query"`
}

type pageResponse[T any] struct {
	Items      []T  `json:"items"`
	Total      int  `json:"total"`
	Limit      int  `json:"limit"`
	Offset     int  `json:"offset"`
	NextOffset *int `json:"next_offset,omitempty"`
}

func parsePageRequest(r *http.Request) (pageRequest, error) {
	limit, err := parsePageLimit(r)
	if err != nil {
		return pageRequest{}, err
	}

	offset := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("offset")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 {
			return pageRequest{}, errInvalidQuery("invalid offset")
		}
		offset = value
	}

	return pageRequest{
		Limit:  limit,
		Offset: offset,
		Query:  strings.TrimSpace(r.URL.Query().Get("q")),
	}, nil
}

func parsePageLimit(r *http.Request) (int, error) {
	limit := defaultPageLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 {
			return 0, errInvalidQuery("invalid limit")
		}
		limit = value
	}
	if limit > maxPageLimit {
		limit = maxPageLimit
	}
	return limit, nil
}

func makePageResponse[T any](items []T, total int, page pageRequest) pageResponse[T] {
	var next *int
	if page.Offset+len(items) < total {
		value := page.Offset + len(items)
		next = &value
	}
	return pageResponse[T]{
		Items:      items,
		Total:      total,
		Limit:      page.Limit,
		Offset:     page.Offset,
		NextOffset: next,
	}
}

type errInvalidQuery string

func (e errInvalidQuery) Error() string {
	return string(e)
}
