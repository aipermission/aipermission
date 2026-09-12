package pipedrain

import (
	"sync"
	"time"
)

func Done(group *sync.WaitGroup) <-chan struct{} {
	done := make(chan struct{})
	if group == nil {
		close(done)
		return done
	}
	go func() {
		group.Wait()
		close(done)
	}()
	return done
}

func Wait(done <-chan struct{}, timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}

func Finish(group *sync.WaitGroup, timeout time.Duration, finish func()) {
	done := Done(group)
	if Wait(done, timeout) {
		finish()
		return
	}
	go func() {
		<-done
		finish()
	}()
}
