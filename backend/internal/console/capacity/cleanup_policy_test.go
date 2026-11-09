package capacity

import "testing"

func TestCleanupPolicyAllowsOnlyFinitePositiveThresholds(t *testing.T) {
	for _, minutes := range []int{10, 20, 30, 45, 60, 120, 180, 360} {
		if !(CleanupPolicy{Enabled: true, IdleMinutes: minutes}).Valid() {
			t.Fatal(minutes)
		}
	}
	for _, minutes := range []int{-1, 0, 1, 11, 100000} {
		if (CleanupPolicy{Enabled: true, IdleMinutes: minutes}).Valid() {
			t.Fatal(minutes)
		}
	}
}
