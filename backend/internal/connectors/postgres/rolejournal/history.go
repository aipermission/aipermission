package rolejournal

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strconv"
)

const HistoryPageLimit = 64

// HistoryPage is inspection only. It does not authorize dispatch, adoption or
// confirmation, and row absence is never evidence of a remote outcome.
type HistoryPage struct {
	Entries             []Entry `json:"entries"`
	HasMore             bool    `json:"has_more"`
	NextAfterResourceID string  `json:"next_after_resource_id"`
}

func (journal *Journal) HistoryForTarget(ctx context.Context, targetID, afterResourceID int64) (HistoryPage, error) {
	if ctx == nil || targetID < 1 || afterResourceID < 0 {
		return HistoryPage{}, errors.New("invalid managed Postgres role history scope")
	}
	entries, err := journal.List(ctx)
	if err != nil {
		return HistoryPage{}, err
	}
	selected := make([]Entry, 0)
	for _, entry := range entries {
		if entry.Record.Intent.Anchor.TargetID == targetID && entry.ResourceID > afterResourceID {
			selected = append(selected, entry)
		}
	}
	slices.SortFunc(selected, func(a, b Entry) int { return cmp.Compare(a.ResourceID, b.ResourceID) })
	page := HistoryPage{Entries: selected, HasMore: len(selected) > HistoryPageLimit}
	if page.HasMore {
		page.Entries = selected[:HistoryPageLimit]
		page.NextAfterResourceID = strconv.FormatInt(page.Entries[len(page.Entries)-1].ResourceID, 10)
	}
	return page, nil
}
