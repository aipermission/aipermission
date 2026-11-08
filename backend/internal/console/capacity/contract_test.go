package capacity

import (
	"testing"
	"time"
)

func TestIdlePolicy(t *testing.T) {
	for _, minutes := range []int{0, 10, 20, 30, 45, 60, 120, 180, 360} {
		if !ValidIdleMinutes(minutes) {
			t.Fatal(minutes)
		}
	}
	for _, minutes := range []int{-1, 1, 11, 600} {
		if ValidIdleMinutes(minutes) {
			t.Fatal(minutes)
		}
	}
	now := time.Now()
	session := Session{CanClose: true, LastActivity: now.Add(-30 * time.Minute).Format(time.RFC3339Nano)}
	if !session.Eligible(now, 30) || session.Eligible(now, 45) {
		t.Fatal("threshold boundary")
	}
	session.LastActivity = "bad"
	if session.Eligible(now, 0) {
		t.Fatal("malformed timestamp")
	}
	session.LastActivity = now.Add(time.Second).Format(time.RFC3339Nano)
	if session.Eligible(now, 0) {
		t.Fatal("future activity")
	}
}
