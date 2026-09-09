package retention

import (
	"context"
	"errors"
	"log"
	"time"
)

const (
	defaultCleanupInterval = time.Hour
	cleanupTimeout         = 30 * time.Second
)

func (s *Service) Start() {
	if !s.available() {
		return
	}
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	alreadyRunning := s.cancel != nil

	s.operationMu.Lock()
	ctx, timeoutCancel := context.WithTimeout(context.Background(), cleanupTimeout)
	deleted, err := s.applyConfigured(ctx)
	timeoutCancel()
	s.operationMu.Unlock()
	s.logResult("retention cleanup", deleted, err)

	if alreadyRunning || s.interval <= 0 {
		return
	}
	workerContext, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.cancel = cancel
	s.done = done
	go s.run(workerContext, done, s.interval)
}

func (s *Service) Stop() {
	if s == nil {
		return
	}
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	cancel, done := s.cancel, s.done
	if cancel == nil {
		return
	}
	cancel()
	if done != nil {
		<-done
	}
	if s.done == done {
		s.cancel, s.done = nil, nil
	}
}

func (s *Service) run(ctx context.Context, done chan<- struct{}, interval time.Duration) {
	defer close(done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runPeriodic(ctx)
		}
	}
}

func (s *Service) runPeriodic(workerContext context.Context) {
	if !s.operationMu.TryLock() {
		return
	}
	defer s.operationMu.Unlock()
	ctx, cancel := context.WithTimeout(workerContext, cleanupTimeout)
	defer cancel()
	deleted, err := s.applyConfigured(ctx)
	if errors.Is(err, context.Canceled) {
		return
	}
	s.logResult("periodic retention cleanup", deleted, err)
}

func (s *Service) logResult(operation string, deleted map[string]int64, err error) {
	if err != nil {
		log.Printf("%s failed workspace=%s error=%v", operation, s.workspaceID, err)
		return
	}
	var total int64
	for _, count := range deleted {
		total += count
	}
	if total > 0 {
		log.Printf("%s completed workspace=%s deleted=%d", operation, s.workspaceID, total)
	}
}
