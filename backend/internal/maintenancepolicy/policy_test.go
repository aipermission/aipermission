package maintenancepolicy

import "testing"

func TestLoadFindsCanonicalRepositoryPolicy(t *testing.T) {
	policy, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if policy.Version != 1 || policy.GoFunction.ProductionMaxLines < 1 || policy.BackendFanout.PackageMax < 1 {
		t.Fatalf("loaded policy is incomplete: %#v", policy)
	}
	if got := MustLoad(); got.Version != policy.Version {
		t.Fatalf("MustLoad version = %d, want %d", got.Version, policy.Version)
	}
}
