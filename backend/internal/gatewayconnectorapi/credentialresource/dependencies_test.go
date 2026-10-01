package credentialresource

import "testing"

func TestIsNilDependencyDoesNotInvokeOrRejectPresentValues(t *testing.T) {
	for _, test := range []struct {
		name   string
		value  any
		absent bool
	}{
		{"nil", nil, true},
		{"pointer", (*int)(nil), true},
		{"map", map[string]int(nil), true},
		{"slice", []int(nil), true},
		{"function", (func())(nil), true},
		{"channel", (chan int)(nil), true},
		{"present pointer", new(int), false},
		{"empty map", map[string]int{}, false},
		{"empty slice", []int{}, false},
		{"present function", func() { t.Fatal("dependency was invoked") }, false},
		{"present channel", make(chan int), false},
		{"struct", struct{}{}, false},
		{"zero scalar", 0, false},
		{"empty string", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := IsNilDependency(test.value); got != test.absent {
				t.Fatalf("absence = %v, want %v", got, test.absent)
			}
		})
	}
}
