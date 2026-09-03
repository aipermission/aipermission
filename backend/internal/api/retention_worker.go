package api

import (
	"context"
	"errors"
	"log"
	"time"
)

const (
	defaultRetentionCleanupInterval = time.Hour
	retentionCleanupTimeout         = 30 * time.Second
)

func (s *Server) initializeRetention(runtime *databaseRuntime) {
	ctx, cancel := context.WithTimeout(context.Background(), retentionCleanupTimeout)
	deleted, err := applyConfiguredRetention(ctx, runtime)
	cancel()
	if err != nil {
		log.Printf("retention cleanup failed workspace=%s error=%v", runtime.id, err)
	} else {
		logRetentionCleanup(runtime, deleted)
	}
	s.startRetentionWorker(runtime)
	s.startConnectorActionRecoveryWorker(runtime)
}

func applyConfiguredRetention(ctx context.Context, runtime *databaseRuntime) (map[string]int64, error) {
	runtime.retentionMu.Lock()
	defer runtime.retentionMu.Unlock()

	settings, err := readRetentionSettings(ctx, runtime)
	if err != nil {
		return nil, err
	}
	return applyRetentionSettings(ctx, runtime, settings)
}

func (s *Server) startRetentionWorker(runtime *databaseRuntime) {
	if runtime == nil || runtime.database == nil || s.retentionInterval <= 0 || runtime.retentionCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	runtime.retentionCancel = cancel
	runtime.retentionDone = done

	go func() {
		defer close(done)
		ticker := time.NewTicker(s.retentionInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.runPeriodicRetention(ctx, runtime)
			}
		}
	}()
}

func (s *Server) runPeriodicRetention(workerContext context.Context, runtime *databaseRuntime) {
	if !runtime.retentionMu.TryLock() {
		return
	}
	defer runtime.retentionMu.Unlock()

	ctx, cancel := context.WithTimeout(workerContext, retentionCleanupTimeout)
	defer cancel()
	settings, err := readRetentionSettings(ctx, runtime)
	if err == nil {
		var deleted map[string]int64
		deleted, err = applyRetentionSettings(ctx, runtime, settings)
		if err == nil {
			logRetentionCleanup(runtime, deleted)
		}
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("periodic retention cleanup failed workspace=%s error=%v", runtime.id, err)
	}
}

func (s *Server) stopRetentionWorker(runtime *databaseRuntime) {
	if runtime == nil || runtime.retentionCancel == nil {
		return
	}
	cancel := runtime.retentionCancel
	done := runtime.retentionDone
	runtime.retentionCancel = nil
	runtime.retentionDone = nil
	cancel()
	if done != nil {
		<-done
	}
}

func logRetentionCleanup(runtime *databaseRuntime, deleted map[string]int64) {
	var total int64
	for _, count := range deleted {
		total += count
	}
	if total > 0 {
		log.Printf("retention cleanup completed workspace=%s deleted=%d", runtime.id, total)
	}
}
