package expirypolicy

import (
	"testing"
	"time"
)

func TestActiveFailsClosedForMalformedValues(t *testing.T) {
	now := time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "permanent", value: "", want: true},
		{name: "future", value: now.Add(time.Minute).Format(time.RFC3339), want: true},
		{name: "expired", value: now.Add(-time.Minute).Format(time.RFC3339), want: false},
		{name: "equal", value: now.Format(time.RFC3339), want: false},
		{name: "malformed", value: "never", want: false},
		{name: "whitespace is not explicit permanent", value: " ", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Active(test.value, now); got != test.want {
				t.Fatalf("Active(%q) = %v, want %v", test.value, got, test.want)
			}
		})
	}
}
