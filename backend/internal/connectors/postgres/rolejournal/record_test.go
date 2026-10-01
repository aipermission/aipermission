package rolejournal

import (
	"encoding/json"
	"strings"
	"testing"

	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
)

func TestRecordRejectsMalformedCanonicalOrForeignEvidence(t *testing.T) {
	base := resourceTest(t)
	cases := map[string]func(*resourcecontract.CredentialResource){
		"missing ID":          func(r *resourcecontract.CredentialResource) { r.ID = 0 },
		"wrong class":         func(r *resourcecontract.CredentialResource) { r.ResourceType = "foreign.v1" },
		"wrong name":          func(r *resourcecontract.CredentialResource) { r.Name += "changed" },
		"wrong fingerprint":   func(r *resourcecontract.CredentialResource) { r.Fingerprint = strings.Repeat("0", 64) },
		"empty JSON":          func(r *resourcecontract.CredentialResource) { r.PublicData = "" },
		"malformed JSON":      func(r *resourcecontract.CredentialResource) { r.PublicData = "{" },
		"trailing object":     func(r *resourcecontract.CredentialResource) { r.PublicData += "{}" },
		"trailing junk":       func(r *resourcecontract.CredentialResource) { r.PublicData += "no" },
		"trailing whitespace": func(r *resourcecontract.CredentialResource) { r.PublicData += "\n" },
		"duplicate key": func(r *resourcecontract.CredentialResource) {
			r.PublicData = strings.Replace(r.PublicData, `"version":1`, `"version":1,"version":1`, 1)
		},
		"unknown field": func(r *resourcecontract.CredentialResource) {
			r.PublicData = strings.Replace(r.PublicData, `"version":1`, `"version":1,"extra":false`, 1)
		},
		"null field": func(r *resourcecontract.CredentialResource) {
			r.PublicData = strings.Replace(r.PublicData, `"role_oid":42`, `"role_oid":null`, 1)
		},
		"large record": func(r *resourcecontract.CredentialResource) { r.PublicData = strings.Repeat(" ", maxRecordBytes+1) },
		"lossy cluster number": func(r *resourcecontract.CredentialResource) {
			r.PublicData = strings.Replace(r.PublicData, `"18446744073709551615"`, `18446744073709551615`, 1)
		},
		"overflow OID": func(r *resourcecontract.CredentialResource) {
			r.PublicData = strings.Replace(r.PublicData, `"role_oid":42`, `"role_oid":4294967296`, 1)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			row := base
			mutate(&row)
			if _, err := parseResource(row); err == nil {
				t.Fatal("invalid/foreign evidence accepted")
			}
		})
	}
}

func TestRecordRejectsInvalidStatusIdentityOrGeneration(t *testing.T) {
	base := resourceTest(t)
	entry, err := parseResource(base)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*Record){
		"version":                 func(r *Record) { r.Version = 2 },
		"generation":              func(r *Record) { r.Generation = "short" },
		"anchor":                  func(r *Record) { r.Intent.Anchor.TargetID = -1 },
		"successor collision":     func(r *Record) { r.RoleOID = r.Intent.Anchor.SuccessorOID },
		"unknown status":          func(r *Record) { r.Status = "absent" },
		"provisioned without OID": func(r *Record) { r.RoleOID = 0 },
		"cleanup without OID":     func(r *Record) { r.RoleOID = 0; r.Status = CleanupIntent },
		"cleaned without OID":     func(r *Record) { r.RoleOID = 0; r.Status = Cleaned },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			record := entry.Record
			mutate(&record)
			if _, err := parseResource(encodeTestRecord(t, base, record)); err == nil {
				t.Fatal("invalid state accepted")
			}
		})
	}
}

func FuzzRoleRecordCanonicalRoundTrip(f *testing.F) {
	intent := Intent{Anchor: testAnchor(), RoleName: "role", OperationID: "abcdef0123456789abcdef0123456789"}
	encoded, err := json.Marshal(Record{Version: 1, Intent: intent, Generation: intent.OperationID, RoleOID: 42, Status: Provisioned})
	if err != nil {
		f.Fatal(err)
	}
	f.Add(string(encoded))
	f.Add(`{"version":1,"version":2}`)
	f.Fuzz(func(t *testing.T, data string) {
		if len(data) > maxRecordBytes+1 {
			return
		}
		row := resourcecontract.CredentialResource{ID: 1, Name: resourceName(intent), ResourceType: recordType, Fingerprint: intentDigest(intent), PublicData: data}
		entry, err := parseResource(row)
		if err != nil {
			return
		}
		canonical, err := json.Marshal(entry.Record)
		if err != nil || string(canonical) != data || entry.Record.Validate() != nil || entry.Record.Intent != intent {
			t.Fatal("accepted noncanonical or foreign record")
		}
	})
}
