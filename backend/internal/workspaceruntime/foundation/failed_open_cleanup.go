package foundation

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"time"
)

type failedOpenDatabase interface {
	Close() error
}

type failedOpenOwnership interface {
	Release() (bool, error)
}

type failedOpenResources struct {
	mu        sync.Mutex
	database  failedOpenDatabase
	ownership failedOpenOwnership
	retrying  bool
}

func (resources *failedOpenResources) Close() error {
	if resources == nil {
		return nil
	}
	var cleanupErr error
	for attempt := 0; attempt < 3; attempt++ {
		closed, err := resources.closeOnce()
		cleanupErr = errors.Join(cleanupErr, err)
		if closed {
			return cleanupErr
		}
		if attempt < 2 {
			time.Sleep(10 * time.Millisecond)
		}
	}
	resources.startRetry()
	return errors.Join(cleanupErr, errors.New("failed workspace open cleanup continues in background"))
}

func (resources *failedOpenResources) closeOnce() (bool, error) {
	resources.mu.Lock()
	defer resources.mu.Unlock()
	if resources.database != nil {
		if err := resources.database.Close(); err != nil {
			return false, fmt.Errorf("close database after failed workspace open: %w", err)
		}
		resources.database = nil
	}
	if resources.ownership != nil {
		released, err := resources.ownership.Release()
		if err != nil {
			return false, fmt.Errorf("release ownership after failed workspace open: %w", err)
		}
		if !released {
			return false, errors.New("release ownership after failed workspace open was not confirmed")
		}
		resources.ownership = nil
	}
	return true, nil
}

func (resources *failedOpenResources) startRetry() {
	resources.mu.Lock()
	if resources.retrying {
		resources.mu.Unlock()
		return
	}
	resources.retrying = true
	resources.mu.Unlock()
	go func() {
		delay := 100 * time.Millisecond
		for {
			closed, err := resources.closeOnce()
			if closed {
				return
			}
			if err != nil {
				log.Printf("failed workspace open cleanup remains pending: %v", err)
			}
			time.Sleep(delay)
			if delay < 5*time.Second {
				delay *= 2
				if delay > 5*time.Second {
					delay = 5 * time.Second
				}
			}
		}
	}()
}
