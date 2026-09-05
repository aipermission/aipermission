package transferjobs

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestRegistrySeparatesFileBatchAndRuntimeIdentities(t *testing.T) {
	var first, second Registry
	file, cancelFile := context.WithCancel(t.Context())
	batch, cancelBatch := context.WithCancel(t.Context())
	other, cancelOther := context.WithCancel(t.Context())
	defer cancelFile()
	defer cancelBatch()
	defer cancelOther()
	first.Files.RegisterCancel(1, cancelFile)
	first.Batches.RegisterCancel(1, cancelBatch)
	second.Files.RegisterCancel(1, cancelOther)
	if !first.Files.Cancel(1) || file.Err() == nil || batch.Err() != nil || other.Err() != nil {
		t.Fatal("cancel crossed identity namespace")
	}
	first.Close()
	if batch.Err() == nil || other.Err() != nil {
		t.Fatal("runtime cancellation crossed workspace boundary")
	}
	second.Close()
	if other.Err() == nil {
		t.Fatal("other workspace did not cancel")
	}
}

func TestRegistryUnregisterKeepsRemainingCapability(t *testing.T) {
	for _, cancelFirst := range []bool{false, true} {
		var group Group
		control := &Control{}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		group.RegisterControl(1, control)
		group.RegisterCancel(1, cancel)
		if cancelFirst {
			group.UnregisterCancel(1)
			if group.Cancel(1) || group.Control(1) != control {
				t.Fatal("cancel removal lost control")
			}
			group.UnregisterControl(1)
		} else {
			group.UnregisterControl(1)
			if group.Control(1) != nil || !group.Cancel(1) || ctx.Err() == nil {
				t.Fatal("control removal lost cancellation")
			}
			group.UnregisterCancel(1)
		}
		if len(group.jobs) != 0 {
			t.Fatal("finished job retained")
		}
		group.UnregisterCancel(2)
		group.UnregisterControl(2)
		if len(group.jobs) != 0 || group.Cancel(2) {
			t.Fatal("unknown job materialized")
		}
	}
}

func TestCancellationCallbacksRunOutsideRegistryLock(t *testing.T) {
	for _, all := range []bool{false, true} {
		var group Group
		done := make(chan struct{})
		group.RegisterCancel(1, func() { group.UnregisterCancel(1) })
		go func() {
			if all {
				group.close()
			} else {
				group.Cancel(1)
			}
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("cancel callback deadlocked registry")
		}
		if len(group.jobs) != 0 {
			t.Fatal("callback cleanup did not remove job")
		}
	}
}

func TestConcurrentRegistrationAndCancellation(t *testing.T) {
	var group Group
	var workers sync.WaitGroup
	for id := int64(1); id <= 50; id++ {
		workers.Go(func() {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			group.RegisterCancel(id, cancel)
			group.RegisterControl(id, &Control{})
			if !group.Cancel(id) || ctx.Err() == nil {
				t.Error("own job not canceled")
			}
			group.UnregisterCancel(id)
			group.UnregisterControl(id)
		})
	}
	workers.Wait()
	if len(group.jobs) != 0 {
		t.Fatal("completed jobs retained")
	}
}

func TestCloseRejectsLateAndConcurrentRegistration(t *testing.T) {
	var registry Registry
	var workers sync.WaitGroup
	for id := int64(1); id <= 50; id++ {
		workers.Go(func() {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			registry.Files.RegisterCancel(id, cancel)
			registry.Files.RegisterControl(id, &Control{})
			registry.Close()
			if ctx.Err() == nil {
				t.Error("shutdown missed concurrent registration")
			}
		})
	}
	workers.Wait()
	registry.Close()
	for _, group := range []*Group{&registry.Files, &registry.Batches} {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		group.RegisterCancel(100, cancel)
		group.RegisterControl(100, &Control{})
		if ctx.Err() == nil || len(group.jobs) != 0 || group.Control(100) != nil {
			t.Fatal("shutdown accepted late work")
		}
	}
}

func TestShutdownWaitsForAcceptedRunnerAndRejectsLateLaunch(t *testing.T) {
	var registry Registry
	ctx, cancel := context.WithCancel(t.Context())
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	if !registry.Files.Launch(1, cancel, func() {
		close(started)
		<-ctx.Done()
		<-release
		close(finished)
	}) {
		t.Fatal("runner was not accepted")
	}
	<-started

	shutdownDone := make(chan bool, 1)
	go func() {
		shutdownDone <- registry.Shutdown(t.Context())
	}()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel runner")
	}
	select {
	case <-shutdownDone:
		t.Fatal("shutdown returned before runner finished")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if !<-shutdownDone {
		t.Fatal("shutdown did not observe drained runner")
	}
	<-finished

	lateCtx, lateCancel := context.WithCancel(t.Context())
	defer lateCancel()
	if registry.Files.Launch(2, lateCancel, func() { t.Error("late runner executed") }) {
		t.Fatal("shutdown accepted late runner")
	}
	if lateCtx.Err() == nil {
		t.Fatal("late runner context was not canceled")
	}
}

func TestShutdownDeadlineKeepsRunnerVisibleToWait(t *testing.T) {
	var registry Registry
	ctx, cancel := context.WithCancel(t.Context())
	release := make(chan struct{})
	if !registry.Batches.Launch(1, cancel, func() {
		<-ctx.Done()
		<-release
	}) {
		t.Fatal("runner was not accepted")
	}
	deadline, stop := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer stop()
	if registry.Shutdown(deadline) {
		t.Fatal("shutdown unexpectedly drained blocked runner")
	}
	close(release)
	if !registry.Wait(t.Context()) {
		t.Fatal("runner did not remain waitable after deadline")
	}
}
