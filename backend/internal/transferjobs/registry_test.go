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

func TestTryLaunchDistinguishesDuplicateFromClosedRegistry(t *testing.T) {
	var registry Registry
	started := make(chan struct{})
	release := make(chan struct{})
	_, cancel := context.WithCancel(t.Context())
	if got := registry.Files.TryLaunch(1, cancel, func() {
		close(started)
		<-release
	}); got != LaunchAccepted {
		t.Fatalf("first launch result = %v, want %v", got, LaunchAccepted)
	}
	<-started

	duplicateCtx, duplicateCancel := context.WithCancel(t.Context())
	if got := registry.Files.TryLaunch(1, duplicateCancel, func() {
		t.Error("duplicate runner executed")
	}); got != LaunchAlreadyRunning {
		t.Fatalf("duplicate launch result = %v, want %v", got, LaunchAlreadyRunning)
	}
	if duplicateCtx.Err() == nil {
		t.Fatal("duplicate runner context was not canceled")
	}

	close(release)
	if !registry.Wait(t.Context()) {
		t.Fatal("accepted runner did not drain")
	}
	registry.Files.beginClose()
	closedCtx, closedCancel := context.WithCancel(t.Context())
	if got := registry.Files.TryLaunch(2, closedCancel, func() {
		t.Error("closed runner executed")
	}); got != LaunchRejectedClosed {
		t.Fatalf("closed launch result = %v, want %v", got, LaunchRejectedClosed)
	}
	if closedCtx.Err() == nil {
		t.Fatal("closed runner context was not canceled")
	}
}

func TestCancelAndWaitObservesAcceptedAndRegisteredWorkers(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		register func(*Group, int64, context.CancelFunc, func())
	}{
		{name: "accepted", register: func(group *Group, id int64, cancel context.CancelFunc, run func()) {
			if got := group.TryLaunch(id, cancel, run); got != LaunchAccepted {
				t.Fatalf("launch result = %v", got)
			}
		}},
		{name: "registered", register: func(group *Group, id int64, cancel context.CancelFunc, run func()) {
			group.RegisterCancel(id, cancel)
			go func() {
				run()
				group.UnregisterCancel(id)
			}()
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			group := &Group{}
			workerCtx, cancel := context.WithCancel(t.Context())
			returned := make(chan struct{})
			testCase.register(group, 41, cancel, func() {
				<-workerCtx.Done()
				close(returned)
			})
			active, drained := group.CancelAndWait(t.Context(), 41)
			if !active || !drained {
				t.Fatalf("cancel result active=%t drained=%t", active, drained)
			}
			select {
			case <-returned:
			default:
				t.Fatal("cancel returned before worker")
			}
		})
	}
}

func TestCancelAndWaitReportsUncertainDrain(t *testing.T) {
	group := &Group{}
	workerCtx, cancel := context.WithCancel(t.Context())
	release := make(chan struct{})
	if got := group.TryLaunch(42, cancel, func() {
		<-workerCtx.Done()
		<-release
	}); got != LaunchAccepted {
		t.Fatalf("launch result = %v", got)
	}
	waitCtx, stopWaiting := context.WithCancel(t.Context())
	stopWaiting()
	active, drained := group.CancelAndWait(waitCtx, 42)
	if !active || drained {
		t.Fatalf("cancel result active=%t drained=%t", active, drained)
	}
	close(release)
	if !group.wait(t.Context()) {
		t.Fatal("worker did not drain after release")
	}
}

func TestTryLaunchRejectsDuplicateRegisteredWorker(t *testing.T) {
	group := &Group{}
	registeredCtx, registeredCancel := context.WithCancel(t.Context())
	group.RegisterCancel(43, registeredCancel)
	duplicateCanceled := false
	if got := group.TryLaunch(43, func() { duplicateCanceled = true }, func() {
		t.Fatal("duplicate worker ran")
	}); got != LaunchAlreadyRunning {
		t.Fatalf("duplicate launch result = %v", got)
	}
	if !duplicateCanceled {
		t.Fatal("duplicate launch context was not canceled")
	}
	registeredCancel()
	<-registeredCtx.Done()
	group.UnregisterCancel(43)
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

func TestWaitOperationsIgnoresWorkspaceMaintenance(t *testing.T) {
	var registry Registry
	maintenanceCtx, cancelMaintenance := context.WithCancel(t.Context())
	if !registry.Maintenance.Launch(1, cancelMaintenance, func() { <-maintenanceCtx.Done() }) {
		t.Fatal("maintenance runner was not accepted")
	}
	fileRelease := make(chan struct{})
	_, cancelFile := context.WithCancel(t.Context())
	if !registry.Files.Launch(1, cancelFile, func() { <-fileRelease }) {
		t.Fatal("file runner was not accepted")
	}
	waitCtx, stopWaiting := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer stopWaiting()
	if registry.WaitOperations(waitCtx) {
		t.Fatal("active file runner was ignored")
	}
	close(fileRelease)
	if !registry.WaitOperations(t.Context()) {
		t.Fatal("maintenance runner blocked operational drain")
	}
	blockedCtx, stopBlockedWait := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer stopBlockedWait()
	if registry.Wait(blockedCtx) {
		t.Fatal("lifecycle drain ignored maintenance runner")
	}
	cancelMaintenance()
	if !registry.Wait(t.Context()) {
		t.Fatal("registry did not drain after maintenance stopped")
	}
}

func TestRepeatedTimedWaitsReuseOneDrainSignal(t *testing.T) {
	var registry Registry
	release := make(chan struct{})
	_, cancel := context.WithCancel(t.Context())
	if !registry.Files.Launch(1, cancel, func() { <-release }) {
		t.Fatal("runner was not accepted")
	}
	registry.BeginShutdown()
	firstDrain := registry.Files.drain
	for range 20 {
		ctx, stop := context.WithTimeout(t.Context(), time.Millisecond)
		if registry.Wait(ctx) {
			stop()
			t.Fatal("blocked runner unexpectedly drained")
		}
		stop()
		if registry.Files.drain != firstDrain {
			t.Fatal("timed wait allocated a new drain signal")
		}
	}
	close(release)
	if !registry.Wait(t.Context()) {
		t.Fatal("runner did not drain")
	}
}
