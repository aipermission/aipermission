package runtimecontrol

import (
	"context"
	"net"
	"net/http"
	"sync"
	"time"
)

const maxAuthEntries = 1024

type Auth struct {
	mu              sync.Mutex
	entries         map[string]authEntry
	delayFailures   int
	lockoutFailures int
}

type authEntry struct {
	failures    int
	lastSeen    time.Time
	lockedUntil time.Time
}

type Window struct {
	mu       sync.Mutex
	limit    int
	window   time.Duration
	attempts map[string][]time.Time
}

func NewWindow(limit int, window time.Duration) *Window {
	return &Window{limit: limit, window: window, attempts: map[string][]time.Time{}}
}

func (l *Window) Allow(key string) bool {
	if l == nil || l.limit < 1 || l.window <= 0 {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-l.window)
	recent := l.attempts[key][:0]
	for _, attempt := range l.attempts[key] {
		if attempt.After(cutoff) {
			recent = append(recent, attempt)
		}
	}
	if len(recent) >= l.limit {
		l.attempts[key] = recent
		return false
	}
	l.attempts[key] = append(recent, now)
	for storedKey, attempts := range l.attempts {
		if len(attempts) == 0 || attempts[len(attempts)-1].Before(cutoff) {
			delete(l.attempts, storedKey)
		}
	}
	return true
}

func NewAuth(delayFailures, lockoutFailures int) *Auth {
	return &Auth{
		entries:         map[string]authEntry{},
		delayFailures:   delayFailures,
		lockoutFailures: lockoutFailures,
	}
}

func (l *Auth) Wait(ctx context.Context, key string) error {
	delay := l.Delay(key)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (l *Auth) RecordFailure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.pruneLocked(now)
	entry := l.entries[key]
	entry.failures++
	entry.lastSeen = now
	if entry.failures >= l.lockoutFailures {
		entry.lockedUntil = now.Add(time.Minute)
	}
	l.entries[key] = entry
	l.pruneLocked(now)
}

func (l *Auth) RecordSuccess(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

func (l *Auth) Delay(key string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.pruneLocked(now)
	entry := l.entries[key]
	if entry.lockedUntil.After(now) {
		return time.Until(entry.lockedUntil)
	}
	if entry.failures < l.delayFailures {
		return 0
	}
	shift := entry.failures - l.delayFailures
	if shift > 4 {
		shift = 4
	}
	delay := time.Duration(1<<shift) * 500 * time.Millisecond
	if delay > 8*time.Second {
		return 8 * time.Second
	}
	return delay
}

func (l *Auth) FailureCount(key string) int {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked(time.Now())
	return l.entries[key].failures
}

func (l *Auth) pruneLocked(now time.Time) {
	for key, entry := range l.entries {
		if now.Sub(entry.lastSeen) > 10*time.Minute {
			delete(l.entries, key)
		}
	}
	for len(l.entries) > maxAuthEntries {
		var oldestKey string
		var oldest time.Time
		for key, entry := range l.entries {
			if oldestKey == "" || entry.lastSeen.Before(oldest) {
				oldestKey = key
				oldest = entry.lastSeen
			}
		}
		delete(l.entries, oldestKey)
	}
}

func Key(r *http.Request, scope string) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || host == "" {
		host = r.RemoteAddr
	}
	return scope + ":" + host
}
