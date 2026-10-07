package actioncapacity_test

import (
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectortargets/actioncapacity"
)

func TestOperatorStorageBudgetIsBoundedAndPreservesOtherLimits(t *testing.T) {
	for _, test := range []struct {
		value string
		bytes int64
	}{
		{"", 256 << 20}, {"256", 256 << 20}, {"1024", 1024 << 20}, {"4096", 4096 << 20},
	} {
		t.Run(test.value, func(t *testing.T) {
			limits, err := actioncapacity.ParseStorageBudget(test.value)
			if err != nil || limits.Bytes != test.bytes || limits.Rows != 20000 || limits.Running != 4 {
				t.Fatalf("budget=%q limits=%#v err=%v", test.value, limits, err)
			}
			t.Setenv(actioncapacity.StorageBudgetEnvironment, test.value)
			actual, err := actioncapacity.RuntimeLimits()
			if err != nil || actual != limits {
				t.Fatalf("runtime limits=%#v err=%v", actual, err)
			}
		})
	}
	for _, value := range []string{"0", "-1", "255", "4097", "1.5", "unlimited", "9223372036854775807", "secret-not-to-echo"} {
		limits, err := actioncapacity.ParseStorageBudget(value)
		if err == nil || limits != (actioncapacity.Limits{}) || strings.Contains(err.Error(), "secret-not-to-echo") {
			t.Fatalf("invalid budget was authorized or echoed: limits=%#v err=%v", limits, err)
		}
	}
}

func TestLargerStorageBudgetDoesNotBypassRowOrRunningGuards(t *testing.T) {
	limits, err := actioncapacity.ParseStorageBudget("1024")
	if err != nil {
		t.Fatal(err)
	}
	usage := actioncapacity.Usage{Rows: 500, Bytes: actioncapacity.MaxBytes + 1, Running: 1}
	if usage.LimitReason(actioncapacity.DefaultLimits()) != "stored_bytes" || usage.LimitReason(limits) != "" {
		t.Fatal("larger storage budget did not address byte-only exhaustion")
	}
	for _, test := range []struct {
		usage  actioncapacity.Usage
		reason string
	}{
		{actioncapacity.Usage{Bytes: limits.Bytes}, ""},
		{actioncapacity.Usage{Bytes: limits.Bytes + 1}, "stored_bytes"},
		{actioncapacity.Usage{Rows: limits.Rows + 1}, "stored_rows"},
		{actioncapacity.Usage{Running: limits.Running + 1}, "running_requests"},
	} {
		if reason := test.usage.LimitReason(limits); reason != test.reason {
			t.Fatalf("usage=%#v reason=%q, want %q", test.usage, reason, test.reason)
		}
	}
}
