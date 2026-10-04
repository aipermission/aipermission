package backups

import (
	"context"
	"errors"
	"strings"
)

var ErrUploadResultExpired = errors.New("the original backup upload record is no longer available")

func (s *Store) GetRecordByProviderFileID(ctx context.Context, providerID int64, providerFileID string) (Record, error) {
	record, err := s.getRecordByProviderFileID(ctx, providerID, strings.TrimSpace(providerFileID))
	if errors.Is(err, ErrRecordNotFound) {
		return Record{}, ErrUploadResultExpired
	}
	return record, err
}
