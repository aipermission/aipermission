package httptransport

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
)

const (
	DefaultPageLimit = 50
	MaxPageLimit     = 100
)

type PageRequest struct {
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
	Query  string `json:"query"`
}

type PageResponse[T any] struct {
	Items      []T  `json:"items"`
	Total      int  `json:"total"`
	Limit      int  `json:"limit"`
	Offset     int  `json:"offset"`
	NextOffset *int `json:"next_offset,omitempty"`
}

func ParsePageRequest(r *http.Request) (PageRequest, error) {
	limit := DefaultPageLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 {
			return PageRequest{}, errors.New("invalid limit")
		}
		limit = min(value, MaxPageLimit)
	}
	offset := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("offset")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 {
			return PageRequest{}, errors.New("invalid offset")
		}
		offset = value
	}
	return PageRequest{Limit: limit, Offset: offset, Query: strings.TrimSpace(r.URL.Query().Get("q"))}, nil
}

func MakePageResponse[T any](items []T, total int, page PageRequest) PageResponse[T] {
	var next *int
	if page.Offset+len(items) < total {
		value := page.Offset + len(items)
		next = &value
	}
	return PageResponse[T]{Items: items, Total: total, Limit: page.Limit, Offset: page.Offset, NextOffset: next}
}
