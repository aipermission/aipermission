package uploadoperation

import (
	"strings"
	"testing"
)

func TestNormalizeClaim(t *testing.T) {
	request, err := NormalizeClaim(ClaimRequest{
		IdempotencyKey: " upload-key ", ProviderID: 7, DatabaseID: " database ",
		WorkspaceInstanceID: " instance ", StreamID: " stream ", SourceInstallationID: " installation ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if request.IdempotencyKey != "upload-key" || request.DatabaseID != "database" || request.WorkspaceInstanceID != "instance" || request.StreamID != "stream" || request.SourceInstallationID != "installation" {
		t.Fatalf("normalized request = %#v", request)
	}
}

func TestNormalizeClaimRejectsIncompleteIdentity(t *testing.T) {
	valid := ClaimRequest{IdempotencyKey: "upload-key", ProviderID: 7, DatabaseID: "database", WorkspaceInstanceID: "instance", StreamID: "stream", SourceInstallationID: "installation"}
	tests := []ClaimRequest{
		{},
		{IdempotencyKey: strings.Repeat("x", maxIdempotencyKeyBytes+1), ProviderID: valid.ProviderID, DatabaseID: valid.DatabaseID, WorkspaceInstanceID: valid.WorkspaceInstanceID, StreamID: valid.StreamID, SourceInstallationID: valid.SourceInstallationID},
		{IdempotencyKey: valid.IdempotencyKey, DatabaseID: valid.DatabaseID, WorkspaceInstanceID: valid.WorkspaceInstanceID, StreamID: valid.StreamID, SourceInstallationID: valid.SourceInstallationID},
		{IdempotencyKey: valid.IdempotencyKey, ProviderID: valid.ProviderID, WorkspaceInstanceID: valid.WorkspaceInstanceID, StreamID: valid.StreamID, SourceInstallationID: valid.SourceInstallationID},
		{IdempotencyKey: valid.IdempotencyKey, ProviderID: valid.ProviderID, DatabaseID: valid.DatabaseID, StreamID: valid.StreamID, SourceInstallationID: valid.SourceInstallationID},
		{IdempotencyKey: valid.IdempotencyKey, ProviderID: valid.ProviderID, DatabaseID: valid.DatabaseID, WorkspaceInstanceID: valid.WorkspaceInstanceID, SourceInstallationID: valid.SourceInstallationID},
		{IdempotencyKey: valid.IdempotencyKey, ProviderID: valid.ProviderID, DatabaseID: valid.DatabaseID, WorkspaceInstanceID: valid.WorkspaceInstanceID, StreamID: valid.StreamID},
	}
	for index, request := range tests {
		if _, err := NormalizeClaim(request); err == nil {
			t.Fatalf("case %d accepted incomplete request %#v", index, request)
		}
	}
}
