package rolejournal

import (
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestTargetAuthorityDoesNotRequireRetiredAdmin(t *testing.T) {
	runtime := authorityRuntime()
	anchor, err := Authority(runtime)
	if err != nil {
		t.Fatal(err)
	}
	for _, profile := range []connectors.CredentialProfileView{
		{}, {ID: 99, TargetID: runtime.Target.ID, ConnectorKind: runtime.Target.ConnectorKind},
	} {
		runtime.Profile = profile
		runtime.Target.Ref = "different-derived-reference"
		if err := VerifyTargetAuthority(runtime.Target, anchor); err != nil {
			t.Fatalf("local evidence required the old admin: %v", err)
		}
		if err := VerifyAuthority(runtime, anchor); err == nil {
			t.Fatal("target-only evidence accidentally authorized remote execution")
		}
	}
}

func TestTargetAuthorityRejectsChangedOrInvalidTarget(t *testing.T) {
	anchor, err := Authority(authorityRuntime())
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*connectors.TargetView){
		"target ID":      func(target *connectors.TargetView) { target.ID++ },
		"project":        func(target *connectors.TargetView) { target.ProjectID++ },
		"kind":           func(target *connectors.TargetView) { target.ConnectorKind = "other" },
		"name":           func(target *connectors.TargetView) { target.Name += " changed" },
		"revision":       func(target *connectors.TargetView) { target.UpdatedAt += " changed" },
		"endpoint":       func(target *connectors.TargetView) { target.Config["host"] = "other.internal" },
		"database":       func(target *connectors.TargetView) { target.Config["database"] = "Main DB" },
		"no database":    func(target *connectors.TargetView) { delete(target.Config, "database") },
		"invalid config": func(target *connectors.TargetView) { target.Config["future"] = func() {} },
		"missing kind":   func(target *connectors.TargetView) { target.ConnectorKind = "" },
		"missing ID":     func(target *connectors.TargetView) { target.ID = 0 },
		"long name":      func(target *connectors.TargetView) { target.Config["database"] = strings.Repeat("x", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			target := authorityRuntime().Target
			mutate(&target)
			if err := VerifyTargetAuthority(target, anchor); err == nil {
				t.Fatal("changed target accepted for terminal evidence")
			}
		})
	}
}

func TestTargetAuthorityRejectsChangedOrIncompleteRecordedDigest(t *testing.T) {
	runtime := authorityRuntime()
	anchor, err := Authority(runtime)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Anchor){
		func(anchor *Anchor) { anchor.TargetDigest = "" },
		func(anchor *Anchor) { anchor.TargetDigest = strings.Repeat("a", 64) },
		func(anchor *Anchor) { anchor.ContextDigest = "" },
		func(anchor *Anchor) { anchor.DatabaseName = "different" },
		func(anchor *Anchor) { anchor.AdminProfileID = 0 },
	} {
		expected := anchor
		mutate(&expected)
		if err := VerifyTargetAuthority(runtime.Target, expected); err == nil {
			t.Fatal("invalid or inconsistent terminal anchor was accepted")
		}
	}
	changed := anchor
	changed.TargetDigest = strings.Repeat("a", 64)
	if err := VerifyCurrentAuthority(anchor, changed); err == nil {
		t.Fatal("full authority ignored a changed target digest")
	}
}
