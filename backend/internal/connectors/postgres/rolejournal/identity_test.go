package rolejournal

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAnchorPreservesExactRemoteIdentity(t *testing.T) {
	anchor := testAnchor()
	if err := anchor.Validate(); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(anchor)
	if err != nil || !strings.Contains(string(encoded), `"cluster_id":"18446744073709551615"`) {
		t.Fatalf("cluster identity lost precision: %s %v", encoded, err)
	}
	var decoded Anchor
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded != anchor {
		t.Fatalf("exact names/cluster were normalized: %#v %v", decoded, err)
	}
}

func TestAnchorRejectsInvalidOrLossyIdentity(t *testing.T) {
	cases := map[string]func(*Anchor){
		"target":           func(a *Anchor) { a.TargetID = 0 },
		"admin":            func(a *Anchor) { a.AdminProfileID = -1 },
		"digest":           func(a *Anchor) { a.ContextDigest = strings.ToUpper(a.ContextDigest) },
		"short digest":     func(a *Anchor) { a.ContextDigest = "ab" },
		"target digest":    func(a *Anchor) { a.TargetDigest = strings.ToUpper(a.TargetDigest) },
		"no target digest": func(a *Anchor) { a.TargetDigest = "" },
		"zero cluster":     func(a *Anchor) { a.ClusterID = "0" },
		"overflow cluster": func(a *Anchor) { a.ClusterID = "18446744073709551616" },
		"signed cluster":   func(a *Anchor) { a.ClusterID = "+12" },
		"padded cluster":   func(a *Anchor) { a.ClusterID = "012" },
		"float cluster":    func(a *Anchor) { a.ClusterID = "1.5" },
		"space cluster":    func(a *Anchor) { a.ClusterID = " 12" },
		"zero database":    func(a *Anchor) { a.DatabaseOID = 0 },
		"zero successor":   func(a *Anchor) { a.SuccessorOID = 0 },
		"empty database":   func(a *Anchor) { a.DatabaseName = "" },
		"database NUL":     func(a *Anchor) { a.DatabaseName = "db\x00other" },
		"long database":    func(a *Anchor) { a.DatabaseName = strings.Repeat("x", 64) },
		"successor bytes":  func(a *Anchor) { a.SuccessorName = "\xff" },
		"long unicode":     func(a *Anchor) { a.SuccessorName = strings.Repeat("\u00e9", 32) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			anchor := testAnchor()
			mutate(&anchor)
			if anchor.Validate() == nil {
				t.Fatal("invalid identity accepted")
			}
		})
	}
}

func TestIntentRejectsInvalidRoleAndOperation(t *testing.T) {
	base := Intent{Anchor: testAnchor(), RoleName: " My Role ", OperationID: "abcdef0123456789abcdef0123456789"}
	if base.validate() != nil || base.Marker() != "aipermission-provision:"+base.OperationID {
		t.Fatal("valid exact intent/marker rejected")
	}
	cases := []Intent{base, base, base, base}
	cases[0].RoleName = ""
	cases[1].RoleName = base.Anchor.SuccessorName
	cases[2].OperationID = strings.ToUpper(base.OperationID)
	cases[3].Anchor.TargetID = 0
	for index, value := range cases {
		if value.validate() == nil {
			t.Fatalf("invalid intent %d accepted", index)
		}
	}
	if validIdentifier(strings.Repeat("x", 63)) != true || validIdentifier("\u00e9") != true {
		t.Fatal("valid PostgreSQL identifier bounds rejected")
	}
}

func TestOverlapSurvivesAliasAndEndpointDrift(t *testing.T) {
	base := Intent{Anchor: testAnchor(), RoleName: "role"}
	cases := []struct {
		name   string
		mutate func(*Intent)
		want   bool
	}{
		{"same", func(*Intent) {}, true},
		{"same cluster alias", func(i *Intent) { i.Anchor.TargetID = 99; i.Anchor.ContextDigest = "other" }, true},
		{"same target new cluster", func(i *Intent) { i.Anchor.ClusterID = "77" }, true},
		{"other database", func(i *Intent) { i.Anchor.DatabaseOID++; i.Anchor.DatabaseName = "another" }, true},
		{"admin drift", func(i *Intent) { i.Anchor.AdminProfileID++; i.Anchor.SuccessorOID++ }, true},
		{"different role", func(i *Intent) { i.RoleName = "Role" }, false},
		{"exact whitespace", func(i *Intent) { i.RoleName = " role " }, false},
		{"different cluster and target", func(i *Intent) { i.Anchor.TargetID++; i.Anchor.ClusterID = "77" }, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			other := base
			test.mutate(&other)
			if base.overlaps(other) != test.want || other.overlaps(base) != test.want {
				t.Fatal("overlap fence is incorrect or asymmetric")
			}
		})
	}
}
