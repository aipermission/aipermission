package idlecleanup

import (
	"context"
	"errors"
	"log"
	"time"
)

func (s *Service) Start() { s.once.Do(func() { go s.run() }) }

func (s *Service) Cancel() { s.cancel() }

func (s *Service) Stop(ctx context.Context) error {
	s.Cancel()
	s.Start()
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func nextHour(now time.Time) time.Time {
	return now.Add(time.Hour - time.Duration(now.Minute())*time.Minute - time.Duration(now.Second())*time.Second - time.Duration(now.Nanosecond()))
}

func (s *Service) run() {
	defer close(s.done)
	runHourly(s.ctx, s.tick)
}

func runHourly(ctx context.Context, tick func()) {
	for {
		timer := time.NewTimer(time.Until(nextHour(time.Now())))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		tick()
	}
}

func (s *Service) tick() {
	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
	defer cancel()
	if err := s.Cleanup(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("automatic console cleanup failed: %v", err)
	}
}
