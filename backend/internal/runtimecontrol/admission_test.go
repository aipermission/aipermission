package runtimecontrol

import (
	"testing"
	"time"
)

func TestAdmissionBoundsRateAndConcurrency(t *testing.T) {
	now := time.Unix(100, 0)
	gate := NewAdmission(2, time.Minute, 1)
	gate.now = func() time.Time { return now }

	release, _, ok := gate.Acquire("token:1")
	if !ok {
		t.Fatal("first acquire was rejected")
	}
	if _, retry, ok := gate.Acquire("token:1"); ok || retry != time.Second {
		t.Fatalf("concurrent acquire = ok %v retry %v", ok, retry)
	}
	release()
	release()
	second, _, ok := gate.Acquire("token:1")
	if !ok {
		t.Fatal("second acquire was rejected")
	}
	second()
	if _, retry, ok := gate.Acquire("token:1"); ok || retry != time.Minute {
		t.Fatalf("rate acquire = ok %v retry %v", ok, retry)
	}
	now = now.Add(time.Minute + time.Second)
	if release, _, ok := gate.Acquire("token:1"); !ok {
		t.Fatal("acquire after window was rejected")
	} else {
		release()
	}
}
