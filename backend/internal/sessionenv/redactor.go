package sessionenv

import (
	"bytes"
	"errors"
	"sync"
)

var redactedValue = []byte("[REDACTED VAULT VALUE]")

// Redactor replaces exact byte sequences while preserving matches split across
// arbitrary stream chunks.
type Redactor struct {
	mu       sync.Mutex
	patterns [][]byte
	pending  []byte
	closed   bool
}

func NewRedactor(patterns [][]byte) (*Redactor, error) {
	unique := map[string]bool{}
	values := make([][]byte, 0, len(patterns))
	for _, pattern := range patterns {
		if len(pattern) == 0 {
			return nil, errors.New("redaction patterns cannot be empty")
		}
		key := string(pattern)
		if unique[key] {
			continue
		}
		unique[key] = true
		values = append(values, bytes.Clone(pattern))
	}
	return &Redactor{patterns: values}, nil
}

func (r *Redactor) Write(chunk []byte) []byte {
	if r == nil || len(chunk) == 0 {
		return bytes.Clone(chunk)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	var output bytes.Buffer
	for _, value := range chunk {
		r.pending = append(r.pending, value)
		r.drain(&output, false)
	}
	return output.Bytes()
}

func (r *Redactor) Close() []byte {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	var output bytes.Buffer
	r.drain(&output, true)
	r.closed = true
	clear(r.pending)
	r.pending = nil
	destroyByteSlices(r.patterns)
	r.patterns = nil
	return output.Bytes()
}

func (r *Redactor) drain(output *bytes.Buffer, final bool) {
	for len(r.pending) > 0 {
		hasPotentialLonger := false
		exact := false
		for _, pattern := range r.patterns {
			if len(r.pending) <= len(pattern) && bytes.Equal(r.pending, pattern[:len(r.pending)]) {
				if len(r.pending) == len(pattern) {
					exact = true
				} else {
					hasPotentialLonger = true
				}
			}
		}
		if !final && hasPotentialLonger {
			return
		}
		if exact {
			output.Write(redactedValue)
			r.pending = r.pending[:0]
			continue
		}
		matchedPrefix := 0
		for _, pattern := range r.patterns {
			if len(pattern) <= len(r.pending) && bytes.Equal(pattern, r.pending[:len(pattern)]) && len(pattern) > matchedPrefix {
				matchedPrefix = len(pattern)
			}
		}
		if matchedPrefix > 0 {
			output.Write(redactedValue)
			r.pending = append(r.pending[:0], r.pending[matchedPrefix:]...)
			continue
		}
		output.WriteByte(r.pending[0])
		r.pending = append(r.pending[:0], r.pending[1:]...)
	}
}
