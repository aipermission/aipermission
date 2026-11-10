package connectors

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
)

func TestFileContentIdentityPinsEmptyAndNonemptyBytes(t *testing.T) {
	for _, content := range []string{"", "release bytes"} {
		digest := sha256.Sum256([]byte(content))
		identity := FileContentIdentity{SizeBytes: int64(len(content)), ChecksumSHA256: hex.EncodeToString(digest[:])}
		if err := identity.Verify(int64(len(content)), identity.ChecksumSHA256); err != nil {
			t.Fatal(err)
		}
		if err := identity.Verify(int64(len(content))+1, identity.ChecksumSHA256); !errors.Is(err, ErrUploadContentChanged) {
			t.Fatalf("changed length accepted: %v", err)
		}
		if err := identity.Verify(int64(len(content)), "changed"); !errors.Is(err, ErrUploadContentChanged) {
			t.Fatalf("changed checksum accepted: %v", err)
		}
	}
	if err := (FileContentIdentity{SizeBytes: -1}).Verify(0, ""); err == nil {
		t.Fatal("invalid identity accepted")
	}
}

func TestFileUploadCatalogRejectsReadOnlyAndUnboundedLimits(t *testing.T) {
	action := ActionDefinition{Name: "upload", Label: "Upload", Description: "Upload one file", Risk: RiskWrite, InputSchema: FileUploadInputSchema(), MaxUploadBytes: MaximumFileUploadBytes}
	if err := ValidateActionDefinitions([]ActionDefinition{action}, "fixture"); err != nil {
		t.Fatal(err)
	}
	for _, limit := range []int64{-1, MaximumFileUploadBytes + 1} {
		invalid := action
		invalid.MaxUploadBytes = limit
		if err := ValidateActionDefinitions([]ActionDefinition{invalid}, "fixture"); err == nil {
			t.Fatal("invalid upload budget accepted")
		}
	}
	action.Risk = RiskRead
	if err := ValidateActionDefinitions([]ActionDefinition{action}, "fixture"); err == nil {
		t.Fatal("file delivery advertised as read-only")
	}
}
