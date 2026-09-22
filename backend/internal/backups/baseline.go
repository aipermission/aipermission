package backups

import (
	"context"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/backups/servicebaseline"
)

type ServiceBaseline = servicebaseline.Baseline

func ReadServiceBaseline(ctx context.Context, db storeDB, baseURL, streamID string) (*ServiceBaseline, error) {
	normalizedURL, streamID, err := serviceBaselineScope(baseURL, streamID)
	if err != nil {
		return nil, err
	}
	return servicebaseline.Read(ctx, db, normalizedURL, streamID, validServiceIdentifier)
}

func WriteServiceBaseline(ctx context.Context, db storeDB, baseURL, streamID string, backup ServiceBackup) error {
	normalizedURL, streamID, err := serviceBaselineScope(baseURL, streamID)
	if err != nil {
		return err
	}
	return servicebaseline.Write(ctx, db, normalizedURL, streamID, ServiceBaseline{
		BackupID: strings.TrimSpace(backup.ID), CreatedAt: strings.TrimSpace(backup.CreatedAt),
	}, validServiceIdentifier)
}

func serviceBaselineScope(baseURL, streamID string) (string, string, error) {
	normalizedURL, err := ValidateServiceURL(baseURL)
	if err != nil {
		return "", "", err
	}
	streamID = strings.TrimSpace(streamID)
	if !validServiceIdentifier(streamID) {
		return "", "", ValidationError("backup stream id is invalid")
	}
	return normalizedURL, streamID, nil
}
