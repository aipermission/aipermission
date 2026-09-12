package pipedrain

import (
	"sync"
	"testing"
	"time"
)

func TestWaitIsBounded(t *testing.T) {
	var group sync.WaitGroup
	group.Add(1)
	if Wait(Done(&group), time.Millisecond) {
		t.Fatal("blocked pipe was reported as drained")
	}
	group.Done()
}

func TestDoneHandlesNilGroup(t *testing.T) {
	if !Wait(Done(nil), time.Second) {
		t.Fatal("nil group did not report an immediate drain")
	}
}

func TestFinishDefersCallbackUntilBlockedGroupDrains(t *testing.T) {
	var group sync.WaitGroup
	group.Add(1)
	finished := make(chan struct{})
	Finish(&group, time.Millisecond, func() { close(finished) })
	select {
	case <-finished:
		t.Fatal("callback ran before the group drained")
	default:
	}
	group.Done()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("callback did not run after the group drained")
	}
}
