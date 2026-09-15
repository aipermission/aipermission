package runtimecontrol

import (
	"sync"
	"time"
)

const maxAdmissionEntries = 4096

// Admission bounds request rate and concurrent work independently for each
// authenticated principal. Rejections do not enqueue work.
type Admission struct {
	mu          sync.Mutex
	limit       int
	window      time.Duration
	concurrency int
	entries     map[string]*admissionEntry
	now         func() time.Time
}

type admissionEntry struct {
	attempts []time.Time
	active   int
	lastSeen time.Time
}

func NewAdmission(limit int, window time.Duration, concurrency int) *Admission {
	return &Admission{limit: limit, window: window, concurrency: concurrency, entries: map[string]*admissionEntry{}, now: time.Now}
}

func (a *Admission) Acquire(key string) (func(), time.Duration, bool) {
	if a == nil || a.limit < 1 || a.window <= 0 || a.concurrency < 1 || key == "" {
		return nil, time.Second, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	a.prune(now)
	entry := a.entries[key]
	if entry == nil {
		if len(a.entries) >= maxAdmissionEntries {
			return nil, time.Second, false
		}
		entry = &admissionEntry{}
		a.entries[key] = entry
	}
	cutoff := now.Add(-a.window)
	recent := entry.attempts[:0]
	for _, attempt := range entry.attempts {
		if attempt.After(cutoff) {
			recent = append(recent, attempt)
		}
	}
	entry.attempts = recent
	entry.lastSeen = now
	if entry.active >= a.concurrency {
		return nil, time.Second, false
	}
	if len(entry.attempts) >= a.limit {
		retry := a.window - now.Sub(entry.attempts[0])
		if retry < time.Second {
			retry = time.Second
		}
		return nil, retry, false
	}
	entry.attempts = append(entry.attempts, now)
	entry.active++
	var once sync.Once
	return func() {
		once.Do(func() {
			a.mu.Lock()
			if current := a.entries[key]; current != nil && current.active > 0 {
				current.active--
				current.lastSeen = a.now()
			}
			a.mu.Unlock()
		})
	}, 0, true
}

func (a *Admission) prune(now time.Time) {
	cutoff := now.Add(-a.window)
	for key, entry := range a.entries {
		if entry.active == 0 && entry.lastSeen.Before(cutoff) {
			delete(a.entries, key)
		}
	}
	for len(a.entries) >= maxAdmissionEntries {
		var oldestKey string
		var oldest time.Time
		for key, entry := range a.entries {
			if entry.active == 0 && (oldestKey == "" || entry.lastSeen.Before(oldest)) {
				oldestKey, oldest = key, entry.lastSeen
			}
		}
		if oldestKey == "" {
			break
		}
		delete(a.entries, oldestKey)
	}
}
