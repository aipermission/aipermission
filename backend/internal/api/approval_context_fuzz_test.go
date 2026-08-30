package api

import (
	"encoding/hex"
	"encoding/json"
	"testing"
)

func FuzzApprovalContextHash(f *testing.F) {
	f.Add([]byte(`{"captured_at":"2026-08-30T12:00:00Z","connector":{"kind":"postgres","version":"0.2"},"action":{"definition_hash":"definition","payload_hash":"payload","context_hash":"context"}}`))
	f.Add([]byte(`{"captured_at":"first","token":{"id":1},"permission":{"rule":"prompt"}}`))
	f.Add([]byte(`{"action":null,"dependencies":[]}`))
	f.Add([]byte(`not-json`))

	f.Fuzz(func(t *testing.T, payload []byte) {
		if len(payload) > 64<<10 {
			return
		}
		var snapshot map[string]any
		if err := json.Unmarshal(payload, &snapshot); err != nil || snapshot == nil {
			return
		}
		hash, err := hashGenericApprovalContext(snapshot)
		if err != nil {
			t.Fatalf("hash approval context: %v", err)
		}
		if len(hash) != 64 {
			t.Fatalf("approval hash length = %d", len(hash))
		}
		if _, err := hex.DecodeString(hash); err != nil {
			t.Fatalf("approval hash is not hexadecimal: %q", hash)
		}
		repeated, err := hashGenericApprovalContext(snapshot)
		if err != nil || repeated != hash {
			t.Fatalf("approval hash is not deterministic: first=%q second=%q err=%v", hash, repeated, err)
		}

		clone := make(map[string]any, len(snapshot)+1)
		for key, value := range snapshot {
			clone[key] = value
		}
		clone["captured_at"] = "changed-without-changing-approval"
		withDifferentCaptureTime, err := hashGenericApprovalContext(clone)
		if err != nil || withDifferentCaptureTime != hash {
			t.Fatalf("captured_at changed approval identity: first=%q second=%q err=%v", hash, withDifferentCaptureTime, err)
		}

		encoded, err := json.Marshal(snapshot)
		if err != nil {
			t.Fatalf("marshal approval context: %v", err)
		}
		_ = connectorApprovalDriftReason(string(encoded), string(encoded))
	})
}
