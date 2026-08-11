package conformance_test

import (
	"encoding/base64"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	s3connector "github.com/aipermission/aipermission/backend/internal/connectors/s3"
)

func TestS3RealService(t *testing.T) {
	requireConformance(t)
	connector := s3connector.New()
	runtime := connectors.RuntimeContext{
		Target: connectors.TargetView{
			ID: 4, Ref: "s3:4:4", ConnectorKind: s3connector.Kind, Name: "conformance-s3",
			Config: map[string]any{
				"connection_mode": "direct",
				"scheme":          "http",
				"host":            fixtureHost("AIPERMISSION_S3_HOST", "127.0.0.1"),
				"port":            fixturePort(t, "AIPERMISSION_S3_PORT", 9000),
				"region":          "us-east-1",
				"bucket":          "aipermission-conformance",
				"path_style":      true,
			},
		},
		Profile: connectors.CredentialProfileView{
			ID: 4, TargetID: 4, ConnectorKind: s3connector.Kind, Kind: "access_key", Label: "conformance",
			Public: map[string]any{"access_key_id": "conformance-access"},
		},
		Secrets:      fixtureSecrets{"secret_access_key": "conformance-secret-key"},
		Capabilities: fixtureCapabilities{},
	}

	assertConnection(t, connector, runtime)
	executeAction(t, connector, runtime, s3connector.ActionBucketInfo, nil)
	const key = "conformance/marker.txt"
	executeAction(t, connector, runtime, s3connector.ActionUploadObject, map[string]any{
		"key": key, "content_base64": base64.StdEncoding.EncodeToString([]byte("s3-conformance")),
		"content_type": "text/plain", "overwrite": true,
	})
	result := executeAction(t, connector, runtime, s3connector.ActionDownloadObject, map[string]any{"key": key, "max_bytes": 1024})
	assertResultContains(t, result, base64.StdEncoding.EncodeToString([]byte("s3-conformance")))
	executeAction(t, connector, runtime, s3connector.ActionDeleteObject, map[string]any{"key": key})
}
