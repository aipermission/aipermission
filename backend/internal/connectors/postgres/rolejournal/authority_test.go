package rolejournal

import (
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func authorityRuntime() connectors.RuntimeContext {
	return connectors.RuntimeContext{
		Target: connectors.TargetView{ID: 1, ProjectID: 8, ConnectorKind: "fixture", Name: "My database",
			Ref: "fixture:1:2", UpdatedAt: "target-revision", Config: map[string]any{"host": "db.internal", "database": " Main DB "}},
		Profile: connectors.CredentialProfileView{ID: 2, TargetID: 1, ConnectorKind: "fixture", Kind: "username_password",
			Label: "Admin", Public: map[string]any{"username": " Main Admin "}, UpdatedAt: "profile-revision", SecretRevision: "secret-revision"},
	}
}

func TestAuthorityBindsCurrentPublicRevisionWithoutTrimming(t *testing.T) {
	runtime := authorityRuntime()
	anchor, err := Authority(runtime)
	if err != nil || anchor.DatabaseName != " Main DB " || anchor.SuccessorName != " Main Admin " {
		t.Fatalf("authority=%#v error=%v", anchor, err)
	}
	if err := VerifyAuthority(runtime, anchor); err != nil {
		t.Fatal(err)
	}
	runtime.Target.Ref = "different-derived-ref"
	if err := VerifyAuthority(runtime, anchor); err != nil {
		t.Fatalf("derived target ref changed authority: %v", err)
	}
	for name, mutate := range map[string]func(*connectors.RuntimeContext){
		"target ID":        func(r *connectors.RuntimeContext) { r.Target.ID++ },
		"project":          func(r *connectors.RuntimeContext) { r.Target.ProjectID++ },
		"target kind":      func(r *connectors.RuntimeContext) { r.Target.ConnectorKind = "other" },
		"target name":      func(r *connectors.RuntimeContext) { r.Target.Name += " changed" },
		"target revision":  func(r *connectors.RuntimeContext) { r.Target.UpdatedAt += " changed" },
		"endpoint":         func(r *connectors.RuntimeContext) { r.Target.Config["host"] = "other.internal" },
		"database":         func(r *connectors.RuntimeContext) { r.Target.Config["database"] = "Main DB" },
		"profile ID":       func(r *connectors.RuntimeContext) { r.Profile.ID++ },
		"profile target":   func(r *connectors.RuntimeContext) { r.Profile.TargetID++ },
		"profile kind":     func(r *connectors.RuntimeContext) { r.Profile.Kind = "other" },
		"profile label":    func(r *connectors.RuntimeContext) { r.Profile.Label += " changed" },
		"username":         func(r *connectors.RuntimeContext) { r.Profile.Public["username"] = "Main Admin" },
		"public metadata":  func(r *connectors.RuntimeContext) { r.Profile.Public["future"] = true },
		"profile revision": func(r *connectors.RuntimeContext) { r.Profile.UpdatedAt += " changed" },
		"secret revision":  func(r *connectors.RuntimeContext) { r.Profile.SecretRevision += " changed" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := authorityRuntime()
			mutate(&changed)
			if err := VerifyAuthority(changed, anchor); err == nil {
				t.Fatal("changed local authority accepted")
			}
		})
	}
}

func TestAuthorityRejectsMissingOrUnserializableAuthority(t *testing.T) {
	for name, mutate := range map[string]func(*connectors.RuntimeContext){
		"target":          func(r *connectors.RuntimeContext) { r.Target.ID = 0 },
		"profile":         func(r *connectors.RuntimeContext) { r.Profile.ID = 0 },
		"kind":            func(r *connectors.RuntimeContext) { r.Target.ConnectorKind, r.Profile.ConnectorKind = "", "" },
		"database absent": func(r *connectors.RuntimeContext) { delete(r.Target.Config, "database") },
		"database type":   func(r *connectors.RuntimeContext) { r.Target.Config["database"] = 12 },
		"username type":   func(r *connectors.RuntimeContext) { r.Profile.Public["username"] = 12 },
		"non JSON":        func(r *connectors.RuntimeContext) { r.Target.Config["future"] = func() {} },
	} {
		t.Run(name, func(t *testing.T) {
			runtime := authorityRuntime()
			mutate(&runtime)
			if _, err := Authority(runtime); err == nil {
				t.Fatal("invalid authority accepted")
			}
		})
	}
}

func TestExplicitAuthoritySnapshotValidationDoesNotTrustInvalidAnchors(t *testing.T) {
	current, err := Authority(authorityRuntime())
	if err != nil {
		t.Fatal(err)
	}
	expected := current
	expected.ClusterID, expected.DatabaseOID, expected.SuccessorOID = "123", 42, 43
	if err := VerifyCurrentAuthority(current, expected); err != nil {
		t.Fatal("remote identity was incorrectly treated as local authority", err)
	}
	for _, side := range []string{"current", "expected"} {
		for _, invalidate := range []func(*Anchor){
			func(a *Anchor) { a.TargetID = 0 },
			func(a *Anchor) { a.AdminProfileID = 0 },
			func(a *Anchor) { a.ContextDigest = "" },
			func(a *Anchor) { a.TargetDigest = "" },
			func(a *Anchor) { a.DatabaseName = "" },
			func(a *Anchor) { a.SuccessorName = "" },
		} {
			left, right := current, expected
			if side == "current" {
				invalidate(&left)
			} else {
				invalidate(&right)
			}
			if err := VerifyCurrentAuthority(left, right); err == nil {
				t.Fatalf("invalid %s authority accepted", side)
			}
		}
	}
}
