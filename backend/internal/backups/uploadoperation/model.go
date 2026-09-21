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
	WorkspaceInstanceID  string
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
	WorkspaceInstanceID  string
	StreamID             string
	SourceInstallationID string
}

func NormalizeClaim(request ClaimRequest) (ClaimRequest, error) {
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	request.DatabaseID = strings.TrimSpace(request.DatabaseID)
	request.WorkspaceInstanceID = strings.TrimSpace(request.WorkspaceInstanceID)
	request.StreamID = strings.TrimSpace(request.StreamID)
	request.SourceInstallationID = strings.TrimSpace(request.SourceInstallationID)
	if !ValidServiceIdentifier(request.IdempotencyKey) {
		return ClaimRequest{}, fmt.Errorf("idempotency_key must be a valid service identifier of at most %d characters", maxIdempotencyKeyBytes)
	}
	if request.ProviderID < 1 || request.DatabaseID == "" || request.WorkspaceInstanceID == "" || request.StreamID == "" || request.SourceInstallationID == "" {
		return ClaimRequest{}, errors.New("backup upload identity is incomplete")
	}
	return request, nil
}

func ValidServiceIdentifier(value string) bool {
	if len(value) < 1 || len(value) > maxIdempotencyKeyBytes {
		return false
	}
	for index, char := range value {
		valid := char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || index > 0 && (char == '.' || char == '_' || char == '-')
		if !valid {
			return false
		}
	}
	return true
}
