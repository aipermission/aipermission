package sshconnector

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func uploadActionRequest() connectors.ActionRequest {
	manifest := connectors.FileUploadManifest{FileName: "release.bin", RemotePath: "/tmp/release.bin", SizeBytes: 3, ChecksumSHA256: strings.Repeat("a", 64)}
	return connectors.ActionRequest{
		Target:     connectors.TargetView{Ref: "ssh:1:2", Name: "Test host", ConnectorKind: Kind},
		Profile:    connectors.CredentialProfileView{ID: 2, ConnectorKind: Kind, Kind: "private_key"},
		ActionName: ActionUploadFile, Input: manifest.ActionInput("upload-fixture"), Reason: "Deploy test file",
	}
}

func TestPrepareFileUploadBindsManifestAndStatusReference(t *testing.T) {
	for _, action := range []string{ActionUploadFile, ActionGetFileUploadStatus} {
		req := uploadActionRequest()
		req.ActionName = action
		prepared, err := New().PrepareAction(t.Context(), req)
		if err != nil {
			t.Fatal(err)
		}
		if prepared.Payload["upload_ref"] != "upload-fixture" || prepared.Summary == "" || prepared.Title == "" {
			t.Fatalf("upload identity lost: %#v", prepared)
		}
		if action == ActionUploadFile {
			if prepared.Risk != connectors.RiskWrite || !reflect.DeepEqual(prepared.Payload, req.Input) || !reflect.DeepEqual(prepared.Preview, req.Input) || !reflect.DeepEqual(prepared.ContextMaterial["upload"], req.Input) {
				t.Fatalf("approved manifest changed: %#v", prepared)
			}
		} else if prepared.Risk != connectors.RiskRead || len(prepared.Payload) != 1 || prepared.ContextMaterial["upload_ref"] != "upload-fixture" {
			t.Fatalf("status lookup retained upload fields: %#v", prepared)
		}
	}
}

func TestPrepareFileUploadRejectsInvalidManifestBeforeDispatch(t *testing.T) {
	for index, test := range []struct {
		field string
		value any
	}{
		{"upload_ref", ""}, {"upload_ref", " leading"}, {"upload_ref", "line\n"}, {"upload_ref", strings.Repeat("r", 129)},
		{"remote_path", "relative"}, {"remote_path", "/tmp/../file"}, {"remote_path", "/"}, {"remote_path", "/tmp/line\n"},
		{"size_bytes", -1}, {"size_bytes", connectors.MaximumFileUploadBytes + 1},
		{"checksum_sha256", "invalid"}, {"checksum_sha256", strings.Repeat("A", 64)},
	} {
		t.Run(test.field+"/"+strconv.Itoa(index), func(t *testing.T) {
			req := uploadActionRequest()
			req.Input[test.field] = test.value
			if _, err := New().PrepareAction(t.Context(), req); err == nil {
				t.Fatalf("invalid %s accepted: %#v", test.field, test.value)
			}
		})
	}
	req := uploadActionRequest()
	req.Reason = " \t"
	if _, err := New().PrepareAction(t.Context(), req); err == nil {
		t.Fatal("upload without a reason accepted")
	}
}
