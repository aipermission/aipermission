package backups

import (
	"encoding/json"
	"testing"
)

type fuzzServiceMetadata struct {
	StreamID   string                  `json:"stream_id"`
	Expected   int64                   `json:"expected_size"`
	Backup     ServiceBackup           `json:"backup"`
	Storage    ServiceStorageUsage     `json:"storage"`
	Policy     ServiceRetentionPolicy  `json:"policy"`
	Preview    ServiceRetentionPreview `json:"preview"`
	KeepLatest int                     `json:"keep_latest"`
}

func FuzzValidateServiceMetadata(f *testing.F) {
	f.Add([]byte(`{"stream_id":"default","expected_size":42,"backup":{"id":"backup-1","stream_id":"default","database_name":"Default","source_installation_id":"install-1","filename":"default.aipdb","size_bytes":42,"sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","created_at":"2026-08-30T12:00:00Z"},"storage":{"used_bytes":42,"backup_count":1,"stream_count":1,"pending_deletions":0},"policy":{"stream_id":"default","enabled":true,"keep_latest":10},"preview":{"stream_id":"default","keep_latest":10,"retain_count":1,"retain_bytes":42,"delete_count":0,"delete_bytes":0},"keep_latest":10}`))
	f.Add([]byte(`{"stream_id":"../escape","backup":{"size_bytes":-1},"storage":{"used_bytes":-1}}`))
	f.Add([]byte(`null`))
	f.Add([]byte(`not-json`))

	f.Fuzz(func(t *testing.T, payload []byte) {
		if len(payload) > 128<<10 {
			return
		}
		var metadata fuzzServiceMetadata
		if err := json.Unmarshal(payload, &metadata); err != nil {
			return
		}
		backupErr := validateServiceBackup(metadata.Backup, metadata.StreamID, metadata.Expected)
		storageErr := validateServiceStorageUsage(metadata.Storage)
		policyErr := validateServiceRetentionPolicy(metadata.Policy, metadata.StreamID)
		previewErr := validateServiceRetentionPreview(metadata.Preview, metadata.StreamID, metadata.KeepLatest)

		encoded, err := json.Marshal(metadata)
		if err != nil {
			t.Fatalf("marshal service metadata: %v", err)
		}
		var repeated fuzzServiceMetadata
		if err := json.Unmarshal(encoded, &repeated); err != nil {
			t.Fatalf("round-trip service metadata: %v", err)
		}
		if (validateServiceBackup(repeated.Backup, repeated.StreamID, repeated.Expected) == nil) != (backupErr == nil) ||
			(validateServiceStorageUsage(repeated.Storage) == nil) != (storageErr == nil) ||
			(validateServiceRetentionPolicy(repeated.Policy, repeated.StreamID) == nil) != (policyErr == nil) ||
			(validateServiceRetentionPreview(repeated.Preview, repeated.StreamID, repeated.KeepLatest) == nil) != (previewErr == nil) {
			t.Fatalf("service metadata validation changed after JSON round trip")
		}
	})
}
