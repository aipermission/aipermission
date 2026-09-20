package uploadoperation

import (
	"errors"
	"fmt"
	"strings"
)

const maxIdempotencyKeyBytes = 128

type Operation struct {
	IdempotencyKey       string
	ProviderID           int64
	DatabaseID           string
	StreamID             string
	SourceInstallationID string
	Status               string
	ProviderFileID       string
	LastError            string
	CreatedAt            string
	UpdatedAt            string
	CompletedAt          *string
}

type ClaimRequest struct {
	IdempotencyKey       string
	ProviderID           int64
	DatabaseID           string
	StreamID             string
	SourceInstallationID string
}

func NormalizeClaim(request ClaimRequest) (ClaimRequest, error) {
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	request.DatabaseID = strings.TrimSpace(request.DatabaseID)
	request.StreamID = strings.TrimSpace(request.StreamID)
	request.SourceInstallationID = strings.TrimSpace(request.SourceInstallationID)
	if request.IdempotencyKey == "" || len(request.IdempotencyKey) > maxIdempotencyKeyBytes {
		return ClaimRequest{}, fmt.Errorf("idempotency_key must contain 1 to %d characters", maxIdempotencyKeyBytes)
	}
	if request.ProviderID < 1 || request.DatabaseID == "" || request.StreamID == "" || request.SourceInstallationID == "" {
		return ClaimRequest{}, errors.New("backup upload identity is incomplete")
	}
	return request, nil
}
